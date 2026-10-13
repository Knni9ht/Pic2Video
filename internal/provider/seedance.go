package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Seedance struct {
	BaseURL        string
	APIKey         string
	ModelID        string
	Client         *http.Client
	DownloadClient *http.Client
}

func NewSeedance(baseURL, apiKey, modelID string) *Seedance {
	return &Seedance{
		BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, ModelID: modelID,
		Client:         &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		DownloadClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (s *Seedance) Submit(ctx context.Context, r Request) (string, error) {
	body := map[string]any{
		"model": s.ModelID,
		"content": []any{
			map[string]any{"type": "text", "text": Prompt(r.Description)},
			map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + r.ImageMIME + ";base64," + base64.StdEncoding.EncodeToString(r.Image)}, "role": "first_frame"},
		},
		"duration": r.Duration, "ratio": r.Ratio, "resolution": r.Resolution, "generate_audio": false,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", &SubmissionError{Message: "无法编码生成请求", Definite: true}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL+"/contents/generations/tasks", bytes.NewReader(encoded))
	if err != nil {
		return "", &SubmissionError{Message: "生成服务地址无效", Definite: true}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	res, err := s.Client.Do(req)
	if err != nil {
		return "", &SubmissionError{Message: "提交结果不明，请先到火山引擎控制台核实，勿重复提交。"}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		definite := res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != http.StatusRequestTimeout
		return "", &SubmissionError{Message: submissionMessage(res.StatusCode, definite), Definite: definite}
	}
	var out struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out) != nil || out.ID == "" {
		return "", &SubmissionError{Message: "生成服务返回了无法识别的提交结果，请先到火山引擎控制台核实，勿重复提交。"}
	}
	return out.ID, nil
}

func submissionMessage(status int, definite bool) string {
	if !definite {
		return "提交结果不明，请先到火山引擎控制台核实，勿重复提交。"
	}
	switch status {
	case 401, 403:
		return "生成服务鉴权或权限校验失败，请检查 API Key 与模型权限。"
	case 400, 422:
		return "生成服务拒绝了参数，请检查模型是否支持单图首帧、所选时长、比例和 720p。"
	case 429:
		return "生成服务请求受限或额度不足，请检查账户后重试。"
	default:
		return "生成服务拒绝创建任务，请检查服务配置。"
	}
}

func (s *Seedance) Check(ctx context.Context, id string) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.BaseURL+"/contents/generations/tasks/"+url.PathEscape(id), nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	res, err := s.Client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("生成服务暂时无法查询")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("生成服务查询失败（HTTP %d）", res.StatusCode)
	}
	var out struct {
		Status  string `json:"status"`
		Content struct {
			VideoURL string `json:"video_url"`
		} `json:"content"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out) != nil {
		return Result{}, fmt.Errorf("生成服务查询返回格式异常")
	}
	switch out.Status {
	case "queued", "running":
		return Result{Status: "running"}, nil
	case "succeeded":
		if out.Content.VideoURL == "" {
			return Result{}, fmt.Errorf("生成服务未提供视频地址")
		}
		return Result{Status: "succeeded", VideoURL: out.Content.VideoURL}, nil
	case "failed", "cancelled", "expired":
		return Result{Status: "failed", Error: "生成任务失败或已过期，请在火山引擎控制台查看详细原因。"}, nil
	default:
		return Result{}, fmt.Errorf("生成服务返回未知状态")
	}
}

func (s *Seedance) Download(ctx context.Context, videoURL string) (io.ReadCloser, error) {
	u, err := url.Parse(videoURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("视频地址无效")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := s.DownloadClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("视频下载连接失败")
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("视频下载失败")
	}
	return res.Body, nil
}
