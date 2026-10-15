package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Knni9ht/Pic2Video/internal/config"
	"github.com/Knni9ht/Pic2Video/internal/provider"
)

// Even an accidental URL change must not let this test spend model quota.
type loopbackTransport struct{ host string }

func (l loopbackTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "http" || r.URL.Host != l.host {
		return nil, fmt.Errorf("test blocked non-fixture destination")
	}
	return http.DefaultTransport.RoundTrip(r)
}

func TestMiniFlowThroughLocalUpstream(t *testing.T) {
	var submissions, queries, downloads atomic.Int32
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v3/contents/generations/tasks":
			submissions.Add(1)
			var body struct {
				Model         string  `json:"model"`
				Duration      int     `json:"duration"`
				Ratio         string  `json:"ratio"`
				Resolution    string  `json:"resolution"`
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
				t.Errorf("decode submission: %v", err)
				http.Error(w, "invalid fixture request", 400)
				return
			}
			if r.Header.Get("Authorization") != "Bearer local-fixture-key" || body.Model != config.DefaultModelID || body.Duration != 5 || body.Resolution != "720p" || body.Ratio != "adaptive" || body.GenerateAudio == nil || *body.GenerateAudio || body.OutputFormat != nil {
				t.Error("unexpected mini model, credentials or generation parameters")
			}
			if len(body.Content) != 2 || !strings.Contains(body.Content[0].Text, "绿色旅行瓶") || body.Content[1].Role != "first_frame" {
				t.Error("missing product prompt or first-frame image")
			} else {
				const prefix = "data:image/png;base64,"
				if !strings.HasPrefix(body.Content[1].ImageURL.URL, prefix) {
					t.Error("missing inline image")
				} else if data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(body.Content[1].ImageURL.URL, prefix)); err != nil {
					t.Error("invalid base64 image")
				} else if _, _, err := validateImage(data); err != nil {
					t.Errorf("invalid submitted image: %v", err)
				}
			}
			writeJSON(w, 200, map[string]string{"id": "fixture-remote-id"})
		case r.Method == "GET" && r.URL.Path == "/api/v3/contents/generations/tasks/fixture-remote-id":
			if queries.Add(1) == 1 {
				writeJSON(w, 200, map[string]string{"status": "running"})
			} else {
				writeJSON(w, 200, map[string]any{"status": "succeeded", "content": map[string]string{"video_url": upstream.URL + "/fixture.mp4"}})
			}
		case r.Method == "GET" && r.URL.Path == "/fixture.mp4":
			downloads.Add(1)
			if r.Header.Get("Authorization") != "" {
				t.Error("API key sent to media endpoint")
			}
			video, err := (&provider.Mock{}).Download(r.Context(), "mock://demo")
			if err != nil {
				t.Error(err)
				http.Error(w, "fixture unavailable", 500)
				return
			}
			defer video.Close()
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = io.Copy(w, video)
		default:
			t.Errorf("unexpected fixture request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	c := config.Config{Password: "demo-password", Provider: "seedance", APIKey: "local-fixture-key", ModelID: config.DefaultModelID, BaseURL: upstream.URL + "/api/v3", DataDir: t.TempDir(), PollInterval: time.Millisecond}
	a, err := New(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Store.Close() })
	p := a.Provider.(*provider.Seedance)
	host := strings.TrimPrefix(upstream.URL, "http://")
	p.Client.Transport = loopbackTransport{host}
	p.DownloadClient.Transport = loopbackTransport{host}
	h := a.Handler(nil)
	if got := request(h, nil, "GET", "/api/jobs").Code; got != 401 {
		t.Fatalf("anonymous jobs: %d", got)
	}
	cookie := loggedIn(t, h)
	data := pngImage(t, 320, 320)
	j := decodeJob(t, submitFields(t, h, cookie, "mini-full-flow-123", "绿色旅行瓶，平稳运镜展示外观。", data, nil))
	startWorker(t, a)
	completed := waitStatus(t, a, j.ID, "succeeded")
	if repeated := decodeJob(t, submitFields(t, h, cookie, "mini-full-flow-123", "绿色旅行瓶，平稳运镜展示外观。", data, nil)); repeated.ID != j.ID {
		t.Fatal("duplicate task")
	}
	if submissions.Load() != 1 || queries.Load() < 2 || downloads.Load() != 1 {
		t.Fatalf("upstream calls: submit=%d query=%d download=%d", submissions.Load(), queries.Load(), downloads.Load())
	}
	for _, route := range []string{completed.ImageURL, completed.VideoURL} {
		if got := request(h, nil, "GET", route).Code; got != 401 {
			t.Fatalf("anonymous media: %d", got)
		}
	}
	r := httptest.NewRequest("GET", completed.VideoURL, nil)
	r.AddCookie(cookie)
	r.Header.Set("Range", "bytes=0-31")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.Len() != 32 {
		t.Fatalf("video range: %d, %d bytes", w.Code, w.Body.Len())
	}
	w = request(h, cookie, "GET", completed.VideoURL+"?download=1")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("video download unavailable")
	}
	w = request(h, cookie, "GET", "/api/jobs?page=1&page_size=12")
	if w.Code != 200 || !strings.Contains(w.Body.String(), j.ID) {
		t.Fatal("task missing from history")
	}
	if got := request(h, cookie, "DELETE", "/api/jobs/"+j.ID).Code; got != 204 {
		t.Fatalf("delete: %d", got)
	}
	if got := request(h, cookie, "GET", "/api/jobs/"+j.ID).Code; got != 404 {
		t.Fatalf("deleted task remained: %d", got)
	}
	if got := request(h, cookie, "POST", "/api/logout").Code; got != 204 {
		t.Fatalf("logout: %d", got)
	}
	if got := request(h, cookie, "GET", "/api/jobs").Code; got != 401 {
		t.Fatalf("session not revoked: %d", got)
	}
}
