package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Knni9ht/Pic2Video/internal/store"
)

// Rename both files before deleting the record so a file lock cannot leave a
// visible task half-deleted. The .delete suffix also makes crashes recoverable.
func (a *App) removeJob(j store.Job) error {
	var moved []string
	rollback := func() {
		for i := len(moved) - 1; i >= 0; i-- {
			if err := os.Rename(moved[i]+".delete", moved[i]); err != nil {
				a.log.Error("无法恢复待删除文件，重启时将再次恢复", "job", j.ID)
			}
		}
	}
	for _, relative := range []string{j.VideoPath, j.ImagePath} {
		if relative == "" {
			continue
		}
		path := filepath.Join(a.Config.DataDir, relative)
		if err := os.Rename(path, path+".delete"); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			rollback()
			return err
		}
		moved = append(moved, path)
	}
	if err := a.Store.Delete(j.ID); err != nil {
		rollback()
		return err
	}
	for _, path := range moved {
		if err := os.Remove(path + ".delete"); err != nil {
			a.log.Warn("待删除文件将在重启后清理", "job", j.ID)
		}
	}
	return nil
}

func (a *App) recoverDeletes() error {
	for _, directory := range []string{"images", "videos"} {
		files, err := filepath.Glob(filepath.Join(a.Config.DataDir, directory, "*.delete"))
		if err != nil {
			return err
		}
		for _, path := range files {
			original := strings.TrimSuffix(path, ".delete")
			id := strings.TrimSuffix(filepath.Base(original), filepath.Ext(original))
			_, err := a.Store.Get(id)
			if store.IsNotFound(err) {
				if err := os.Remove(path); err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else {
				if err := os.Rename(path, original); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
