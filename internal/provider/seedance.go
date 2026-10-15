package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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
	if r.Ratio != "adaptive" {
		return "", &SubmissionError{Message: "当前商品首帧流程使用跟随原图（adaptive）。", Definite: true, Details: Diagnostic{Stage: "submit"}}
	}
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
		return "", &SubmissionError{Message: "无法编码生成请求", Definite: true, Details: Diagnostic{Stage: "submit"}}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL+"/contents/generations/tasks", bytes.NewReader(encoded))
	if err != nil {
		return "", &SubmissionError{Message: "生成服务地址无效", Definite: true, Details: Diagnostic{Stage: "submit"}}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	res, err := s.Client.Do(req)
	if err != nil {
		return "", &SubmissionError{Message: submissionMessage(0, false), Details: Diagnostic{Stage: "submit"}}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		definite := res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != http.StatusRequestTimeout
		code := readErrorCode(res.Body)
		message := submissionMessage(res.StatusCode, definite)
		if definite {
			message = serviceMessage(code, res.StatusCode, message)
		}
		return "", &SubmissionError{Message: message, Definite: definite, Details: Diagnostic{Stage: "submit", HTTPStatus: res.StatusCode, UpstreamCode: code}}
	}
	var out struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out) != nil || out.ID == "" {
		return "", &SubmissionError{Message: "生成服务返回了无法识别的提交结果，请先到火山引擎控制台核实，勿重复提交。", Details: Diagnostic{Stage: "submit", HTTPStatus: res.StatusCode}}
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
		return "生成服务拒绝了参数，请检查 Seedance 2.0 mini、单图首帧、5/10 秒、跟随原图和 720p 配置。"
	case 429:
		return "生成服务请求受限或额度不足，请检查账户后重试。"
	default:
		return "生成服务拒绝创建任务，请检查服务配置。"
	}
}

type upstreamError struct {
	Code string `json:"code"`
}

func readErrorCode(r io.Reader) string {
	var out struct {
		Error upstreamError `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&out) != nil {
		return ""
	}
	return safeErrorCode(out.Error.Code)
}

func safeErrorCode(code string) string {
	if len(code) > 128 {
		return "UnrecognizedErrorCode"
	}
	for _, c := range code {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return "UnrecognizedErrorCode"
		}
	}
	return code
}

func serviceMessage(code string, status int, fallback string) string {
	switch {
	case code == "OperationDenied.ServiceOverdue" || strings.Contains(code, "InsufficientBalance"):
		return "生成服务账户余额不足或已欠费，请充值后检查模型开通状态。"
	case code == "ModelNotOpen":
		return "尚未开通所配置的模型，请在火山方舟控制台开通 Seedance 2.0 mini。"
	case code == "SetLimitExceeded":
		return "生成服务已达到设置的用量上限，请检查安心体验模式和模型用量限制；调整后再手动创建新任务。"
	case strings.HasPrefix(code, "InvalidEndpointOrModel"):
		return "模型 ID 或推理接入点不可用，请检查 ID、项目空间与 API Key 的模型权限。"
	case strings.Contains(code, "SensitiveContentDetected") || strings.Contains(code, "RiskDetection"):
		return "图片、描述或生成结果未通过服务内容校验，请更换素材或调整描述。"
	case strings.HasPrefix(code, "AuthenticationError") || code == "InvalidApiKey" || status == 401:
		return "生成服务鉴权失败，请检查 API Key 是否有效且已启用。"
	case strings.Contains(code, "QuotaExceeded") || strings.Contains(code, "InsufficientQuota"):
		return "生成服务额度不足，请检查账户额度、资源包及模型用量限制。"
	case strings.HasPrefix(code, "RateLimitExceeded") || status == 429:
		return "生成服务请求受限，请检查模型并发和请求频率后重试。"
	case strings.HasPrefix(code, "OperationDenied") || strings.HasPrefix(code, "AccessDenied") || status == 403:
		return "生成服务权限校验失败，请检查模型开通、项目空间及 API Key 的模型权限和 IP 白名单。"
	case strings.HasPrefix(code, "InvalidParameter") || strings.HasPrefix(code, "MissingParameter"):
		return "生成服务拒绝了参数，请检查图片、Seedance 2.0 mini、5/10 秒、跟随原图和 720p 配置。"
	default:
		return fallback
	}
}

func (s *Seedance) Check(ctx context.Context, id string) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.BaseURL+"/contents/generations/tasks/"+url.PathEscape(id), nil)
	if err != nil {
		return Result{}, &ServiceError{Message: "生成服务查询地址无效", Details: Diagnostic{Stage: "check"}}
	}
	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	res, err := s.Client.Do(req)
	if err != nil {
		return Result{}, &ServiceError{Message: "生成服务暂时无法查询", Details: Diagnostic{Stage: "check"}}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		code := readErrorCode(res.Body)
		return Result{}, &ServiceError{Message: serviceMessage(code, res.StatusCode, "生成服务查询失败"), Details: Diagnostic{Stage: "check", HTTPStatus: res.StatusCode, UpstreamCode: code}}
	}
	var out struct {
		Status  string        `json:"status"`
		Error   upstreamError `json:"error"`
		Content struct {
			VideoURL string `json:"video_url"`
		} `json:"content"`
	}
	details := Diagnostic{Stage: "check", HTTPStatus: res.StatusCode}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out) != nil {
		return Result{}, &ServiceError{Message: "生成服务查询返回格式异常", Details: details}
	}
	details.UpstreamCode = safeErrorCode(out.Error.Code)
	switch out.Status {
	case "queued", "running":
		return Result{Status: "running", Details: details}, nil
	case "succeeded":
		if out.Content.VideoURL == "" {
			return Result{}, &ServiceError{Message: "生成服务未提供视频地址", Details: details}
		}
		return Result{Status: "succeeded", VideoURL: out.Content.VideoURL, Details: details}, nil
	case "failed":
		return Result{Status: "failed", Error: serviceMessage(details.UpstreamCode, 0, "生成任务失败，请在火山方舟控制台查看详细原因。"), Details: details}, nil
	case "cancelled":
		return Result{Status: "failed", Error: "远程生成任务已取消，请在火山方舟控制台核实。", Details: details}, nil
	case "expired":
		return Result{Status: "failed", Error: "远程生成任务已过期，请在火山方舟控制台核实。", Details: details}, nil
	default:
		return Result{}, &ServiceError{Message: "生成服务返回未知状态", Details: details}
	}
}

func (s *Seedance) Download(ctx context.Context, videoURL string) (io.ReadCloser, error) {
	u, err := url.Parse(videoURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return nil, &ServiceError{Message: "视频地址无效", Details: Diagnostic{Stage: "download"}}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		return nil, &ServiceError{Message: "视频地址无效", Details: Diagnostic{Stage: "download"}}
	}
	res, err := s.DownloadClient.Do(req)
	if err != nil {
		return nil, &ServiceError{Message: "视频下载连接失败", Details: Diagnostic{Stage: "download"}}
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, &ServiceError{Message: "视频下载失败", Details: Diagnostic{Stage: "download", HTTPStatus: res.StatusCode}}
	}
	return res.Body, nil
}
