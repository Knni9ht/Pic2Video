package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSeedanceWireContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer server-secret" {
			t.Error("missing authorization")
		}
		if r.Method == "POST" && r.URL.Path == "/api/v3/contents/generations/tasks" {
			var body struct {
				Model         string `json:"model"`
				Duration      int    `json:"duration"`
				Resolution    string `json:"resolution"`
				Ratio         string `json:"ratio"`
				GenerateAudio bool   `json:"generate_audio"`
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
			if body.Model != "my-model" || body.Duration != 5 || body.Resolution != "720p" || body.Ratio != "9:16" || body.GenerateAudio {
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
	p := NewSeedance(server.URL+"/api/v3", "server-secret", "my-model")
	id, err := p.Submit(context.Background(), Request{Description: "商品说明", Image: []byte("image"), ImageMIME: "image/png", Duration: 5, Resolution: "720p", Ratio: "9:16"})
	if err != nil || id != "task-123" {
		t.Fatalf("submit: %s %v", id, err)
	}
	result, err := p.Check(context.Background(), id)
	if err != nil || result.Status != "succeeded" || result.VideoURL != "https://cdn.example/video.mp4" {
		t.Fatalf("check: %+v %v", result, err)
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
			_, err := p.Submit(context.Background(), Request{})
			var submission *SubmissionError
			if !errors.As(err, &submission) || submission.Definite != test.definite {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("upstream details leaked")
			}
		})
	}
}
