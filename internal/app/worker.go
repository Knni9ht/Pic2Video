package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Knni9ht/Pic2Video/internal/provider"
	"github.com/Knni9ht/Pic2Video/internal/store"
)

func (a *App) save(j *store.Job) bool {
	if err := a.Store.Save(j); err != nil {
		a.log.Error("无法保存任务状态", "job", j.ID, "remote_id", j.RemoteID)
		return false
	}
	return true
}
func (a *App) fail(j *store.Job, status, message string) {
	j.Status = status
	j.Error = message
	a.save(j)
}

func (a *App) process(ctx context.Context, j *store.Job) {
	if j.Status == "queued" {
		data, err := os.ReadFile(filepath.Join(a.Config.DataDir, j.ImagePath))
		if err != nil {
			a.fail(j, "failed", "商品图片无法读取，请重新上传。 ")
			return
		}
		j.Status = "submitting"
		if !a.save(j) {
			return
		}
		id, err := a.Provider.Submit(ctx, provider.Request{Description: j.Description, Image: data, ImageMIME: j.ImageMIME, Duration: j.Duration, Ratio: j.Ratio, Resolution: j.Resolution})
		if err != nil {
			var rejection *provider.SubmissionError
			if errors.As(err, &rejection) && rejection.Definite {
				a.fail(j, "failed", rejection.Message)
			} else {
				a.fail(j, "unknown", "提交结果不明，可能已产生收费任务。请先到火山引擎控制台核实，勿重复提交。")
			}
			return
		}
		j.RemoteID = id
		j.Status = "running"
		if !a.save(j) {
			return
		}
	}
	if j.Status == "running" {
		failures := 0
		deadline := time.Now().Add(2 * time.Hour)
		for ctx.Err() == nil {
			if time.Now().After(deadline) {
				a.fail(j, "unknown", "生成时间超过 2 小时，已停止轮询。请先到火山引擎控制台核实结果，勿重复提交。")
				return
			}
			result, err := a.Provider.Check(ctx, j.RemoteID)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				failures++
				if failures >= 3 {
					a.fail(j, "unknown", "连续查询失败，无法确认生成结果。请先到火山引擎控制台核实，勿重复提交。")
					return
				}
			} else {
				failures = 0
				switch result.Status {
				case "succeeded":
					j.RemoteURL = result.VideoURL
					j.Status = "downloading"
					if !a.save(j) {
						return
					}
				case "failed":
					a.fail(j, "failed", result.Error)
					return
				}
			}
			if j.Status == "downloading" {
				break
			}
			if !pause(ctx, a.Config.PollInterval) {
				return
			}
		}
	}
	if j.Status != "downloading" || ctx.Err() != nil {
		return
	}
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return
		}
		if err := a.download(ctx, j); err == nil {
			j.Status = "succeeded"
			j.Error = ""
			a.save(j)
			return
		}
		if ctx.Err() != nil {
			return
		}
		if attempt < 2 {
			if !pause(ctx, a.Config.PollInterval*time.Duration(attempt+1)) {
				return
			}
			// Refresh an expiring video URL; never create another paid task.
			if result, err := a.Provider.Check(ctx, j.RemoteID); err == nil && result.Status == "succeeded" {
				j.RemoteURL = result.VideoURL
				if !a.save(j) {
					return
				}
			}
		}
	}
	a.fail(j, "failed", "视频已生成，但本地保存连续失败。请先在火山引擎控制台下载已有结果；重新生成会创建新任务。")
}

func (a *App) download(ctx context.Context, j *store.Job) error {
	relativePath := filepath.Join("videos", j.ID+".mp4")
	finalPath := filepath.Join(a.Config.DataDir, relativePath)
	// A crash after the rename may leave a valid file before status was persisted.
	if f, err := os.Open(finalPath); err == nil {
		valid := validMP4(f)
		f.Close()
		if valid {
			j.VideoPath = relativePath
			return nil
		}
		return fmt.Errorf("existing video invalid")
	}
	reader, err := a.Provider.Download(ctx, j.RemoteURL)
	if err != nil {
		return err
	}
	defer reader.Close()
	f, err := os.CreateTemp(filepath.Join(a.Config.DataDir, "videos"), j.ID+"-*.part")
	if err != nil {
		return err
	}
	tempPath := f.Name()
	defer os.Remove(tempPath)
	const maxVideoBytes = 512 << 20
	n, copyErr := io.Copy(f, io.LimitReader(reader, maxVideoBytes+1))
	if copyErr == nil {
		copyErr = f.Sync()
	}
	if copyErr == nil {
		_, copyErr = f.Seek(0, 0)
	}
	if copyErr == nil && (n > maxVideoBytes || !validMP4(f)) {
		copyErr = errors.New("invalid or oversized mp4")
	}
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return err
	}
	j.VideoPath = relativePath
	return nil
}
func validMP4(r io.Reader) bool {
	var header [12]byte
	_, err := io.ReadFull(r, header[:])
	return err == nil && string(header[4:8]) == "ftyp"
}
func pause(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
