package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Knni9ht/Pic2Video/internal/config"
)

func TestSeedanceWireContract(t *testing.T) {
	for _, duration := range []int{5, 10} {
		t.Run(fmt.Sprintf("%d seconds", duration), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer server-secret" {
					t.Error("missing authorization")
				}
				if r.Method == "POST" && r.URL.Path == "/api/v3/contents/generations/tasks" {
					var body struct {
						Model         string  `json:"model"`
						Duration      int     `json:"duration"`
						Resolution    string  `json:"resolution"`
						Ratio         string  `json:"ratio"`
						GenerateAudio *bool   `json:"generate_audio"`
						OutputFormat  *string `json:"output_format"`
						Content       []struct {
							Type     string `json:"type"`
							Text     string `json:"text"`
							Role     string `json:"role"`
							ImageURL struct {
								URL string `json:"url"`
							} `json:"image_url"`
						} `json:"content"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if body.Model != config.DefaultModelID || body.Duration != duration || body.Resolution != "720p" || body.Ratio != "adaptive" || body.GenerateAudio == nil || *body.GenerateAudio || body.OutputFormat != nil {
						t.Errorf("bad generation settings: %+v", body)
					}
					if len(body.Content) != 2 || !strings.Contains(body.Content[0].Text, "商品说明") || body.Content[1].Role != "first_frame" || body.Content[1].ImageURL.URL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("image")) {
						t.Errorf("bad content: %+v", body.Content)
					}
					_, _ = w.Write([]byte(`{"id":"task-123"}`))
					return
				}
				if r.Method == "GET" && r.URL.Path == "/api/v3/contents/generations/tasks/task-123" {
					_, _ = w.Write([]byte(`{"id":"task-123","status":"succeeded","content":{"video_url":"https://cdn.example/video.mp4"}}`))
					return
				}
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				w.WriteHeader(404)
			}))
			defer server.Close()
			p := NewSeedance(server.URL+"/api/v3", "server-secret", config.DefaultModelID)
			id, err := p.Submit(context.Background(), Request{Description: "商品说明", Image: []byte("image"), ImageMIME: "image/png", Duration: duration, Resolution: "720p", Ratio: "adaptive"})
			if err != nil || id != "task-123" {
				t.Fatalf("submit: %s %v", id, err)
			}
			result, err := p.Check(context.Background(), id)
			if err != nil || result.Status != "succeeded" || result.VideoURL != "https://cdn.example/video.mp4" {
				t.Fatalf("check: %+v %v", result, err)
			}
		})
	}
}

func TestSeedanceErrorDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, method, body, code, message string
		status                            int
		failed                            bool
	}{
		{"unopened model", "POST", `{"error":{"code":"ModelNotOpen","message":"secret"}}`, "ModelNotOpen", "开通", 404, false},
		{"overdue account", "POST", `{"error":{"code":"OperationDenied.ServiceOverdue","message":"secret"}}`, "OperationDenied.ServiceOverdue", "余额", 403, false},
		{"bad parameter", "POST", `{"error":{"code":"InvalidParameter.Ratio","message":"secret"}}`, "InvalidParameter.Ratio", "参数", 400, false},
		{"unknown error", "POST", `{"error":{"code":"NewProviderError","message":"secret"}}`, "NewProviderError", "拒绝", 400, false},
		{"unsafe code", "POST", `{"error":{"code":"invalid\nsecret","message":"secret"}}`, "UnrecognizedErrorCode", "拒绝", 400, false},
		{"query auth", "GET", `{"error":{"code":"AuthenticationError","message":"secret"}}`, "AuthenticationError", "鉴权", 401, false},
		{"query forbidden", "GET", `{"error":{"code":"AccessDenied","message":"secret"}}`, "AccessDenied", "权限", 403, false},
		{"query throttled", "GET", `{"error":{"code":"RateLimitExceeded","message":"secret"}}`, "RateLimitExceeded", "受限", 429, false},
		{"async failure", "GET", `{"status":"failed","error":{"code":"InputImageSensitiveContentDetected","message":"secret"}}`, "InputImageSensitiveContentDetected", "内容校验", 200, true},
		{"experience limit", "GET", `{"status":"failed","error":{"code":"SetLimitExceeded","message":"secret"}}`, "SetLimitExceeded", "安心体验", 200, true},
		{"expired", "GET", `{"status":"expired"}`, "", "过期", 200, true},
		{"cancelled", "GET", `{"status":"cancelled"}`, "", "取消", 200, true},
		{"missing video", "GET", `{"status":"succeeded","content":{}}`, "", "视频地址", 200, false},
		{"malformed query", "GET", `secret`, "", "格式异常", 200, false},
		{"unexpected state", "GET", `{"status":"new-status"}`, "", "未知状态", 200, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			p := NewSeedance(server.URL, "key", "model")
			var details Diagnostic
			var message string
			if test.method == "POST" {
				_, err := p.Submit(context.Background(), Request{Ratio: "adaptive"})
				var rejection *SubmissionError
				if !errors.As(err, &rejection) || !rejection.Definite {
					t.Fatalf("unexpected submission error: %v", err)
				}
				details, message = rejection.Details, rejection.Message
			} else {
				result, err := p.Check(context.Background(), "task")
				if test.failed {
					if err != nil || result.Status != "failed" {
						t.Fatalf("unexpected result: %+v %v", result, err)
					}
					details, message = result.Details, result.Error
				} else {
					var failure *ServiceError
					if !errors.As(err, &failure) {
						t.Fatalf("missing typed query error: %v", err)
					}
					details, message = failure.Details, failure.Message
				}
			}
			if details.HTTPStatus != test.status || details.UpstreamCode != test.code || !strings.Contains(message, test.message) {
				t.Fatalf("unexpected diagnostic or message: %+v %s", details, message)
			}
			if strings.Contains(message, "secret") {
				t.Fatal("raw upstream message leaked")
			}
		})
	}
}

func TestSeedanceRejectsFixedRatioBeforeCalling(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called.Store(true) }))
	defer server.Close()
	_, err := NewSeedance(server.URL, "key", "model").Submit(context.Background(), Request{Ratio: "9:16"})
	var rejection *SubmissionError
	if !errors.As(err, &rejection) || !rejection.Definite || called.Load() {
		t.Fatalf("invalid ratio reached upstream: %v", err)
	}
}

func TestSubmissionUncertainty(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		body     string
		definite bool
	}{
		{"parameter rejected", 400, `{"error":{"message":"secret"}}`, true},
		{"unauthorized", 401, `{"error":{"message":"secret"}}`, true},
		{"forbidden", 403, `{"error":{"code":"AccessDenied","message":"secret"}}`, true},
		{"rate limited", 429, `{"error":{"code":"RateLimitExceeded","message":"secret"}}`, true},
		{"upstream failure", 503, "secret", false},
		{"timeout", 408, "secret", false},
		{"malformed success", 200, "bad-json", false},
		{"missing id", 200, `{}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			p := NewSeedance(server.URL, "key", "model")
			_, err := p.Submit(context.Background(), Request{Ratio: "adaptive"})
			var submission *SubmissionError
			if !errors.As(err, &submission) || submission.Definite != test.definite {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("upstream details leaked")
			}
			if submission.Details.Stage != "submit" || submission.Details.HTTPStatus != test.status {
				t.Fatalf("missing diagnostics: %+v", submission.Details)
			}
		})
	}
}
