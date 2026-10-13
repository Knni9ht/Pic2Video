package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Knni9ht/Pic2Video/internal/config"
	"github.com/Knni9ht/Pic2Video/internal/provider"
	"github.com/Knni9ht/Pic2Video/internal/store"
)

func newTestApp(t *testing.T) (*App, http.Handler) {
	t.Helper()
	a, err := New(config.Config{Password: "demo-password", Provider: "mock", DataDir: t.TempDir(), PollInterval: time.Millisecond}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Store.Close() })
	return a, a.Handler(nil)
}
func loggedIn(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"password":"demo-password"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	c := w.Result().Cookies()[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatal("insecure cookie")
	}
	return c
}
func pngImage(t *testing.T, width, height int) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, width, height))
	im.Set(0, 0, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func submit(t *testing.T, h http.Handler, cookie *http.Cookie, key, description string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	_ = m.WriteField("description", description)
	_ = m.WriteField("duration", "5")
	_ = m.WriteField("ratio", "9:16")
	_ = m.WriteField("resolution", "720p")
	f, err := m.CreateFormFile("image", "product.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write(data)
	_ = m.Close()
	r := httptest.NewRequest("POST", "/api/jobs", &body)
	r.Header.Set("Content-Type", m.FormDataContentType())
	r.Header.Set("Idempotency-Key", key)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func decodeJob(t *testing.T, w *httptest.ResponseRecorder) store.Job {
	t.Helper()
	if w.Code != 202 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var j store.Job
	if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	return j
}
func request(h http.Handler, cookie *http.Cookie, method, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func waitStatus(t *testing.T, a *App, id, status string) store.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, err := a.Store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status == status {
			return j
		}
		time.Sleep(time.Millisecond * 5)
	}
	j, _ := a.Store.Get(id)
	t.Fatalf("want %s, got %s: %s", status, j.Status, j.Error)
	return j
}
func startWorker(t *testing.T, a *App) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); a.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
}

func TestLoginProtectionAndRateLimit(t *testing.T) {
	_, h := newTestApp(t)
	for _, path := range []string{"/api/jobs", "/api/jobs/a/image", "/api/jobs/a/video"} {
		if got := request(h, nil, "GET", path).Code; got != 401 {
			t.Fatalf("%s unauth status %d", path, got)
		}
	}
	w := request(h, nil, "GET", "/api/session")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"authenticated":false`) {
		t.Fatal(w.Body.String())
	}
	for i := 0; i < 6; i++ {
		r := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"password":"wrong"}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 401
		if i == 5 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
}

func TestMockFlowAndIdempotency(t *testing.T) {
	a, h := newTestApp(t)
	a.Provider = &provider.Mock{Delay: 15 * time.Millisecond}
	cookie := loggedIn(t, h)
	data := pngImage(t, 360, 640)
	j := decodeJob(t, submit(t, h, cookie, "request-123", "演示商品", data))
	duplicate := decodeJob(t, submit(t, h, cookie, "request-123", "演示商品", data))
	if duplicate.ID != j.ID {
		t.Fatal("duplicate submission created a new task")
	}
	if w := submit(t, h, cookie, "request-123", "另一个商品", data); w.Code != 409 {
		t.Fatalf("conflict: %d", w.Code)
	}
	if w := request(h, cookie, "DELETE", "/api/jobs/"+j.ID); w.Code != 409 {
		t.Fatal("active task deleted")
	}
	startWorker(t, a)
	j = waitStatus(t, a, j.ID, "succeeded")
	if j.VideoPath == "" || j.VideoURL == "" {
		t.Fatal("missing local video")
	}
	r := httptest.NewRequest("GET", j.VideoURL, nil)
	r.AddCookie(cookie)
	r.Header.Set("Range", "bytes=0-31")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.Len() != 32 {
		t.Fatalf("range: %d, len %d", w.Code, w.Body.Len())
	}
	w = request(h, cookie, "GET", j.VideoURL+"?download=1")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("missing download")
	}
	w = request(h, cookie, "GET", "/api/jobs?page=1&page_size=12")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatal(w.Body.String())
	}
	if w = request(h, cookie, "DELETE", "/api/jobs/"+j.ID); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(a.Config.DataDir, j.ImagePath)); !os.IsNotExist(err) {
		t.Fatal("image not deleted")
	}
	if _, err := os.Stat(filepath.Join(a.Config.DataDir, j.VideoPath)); !os.IsNotExist(err) {
		t.Fatal("video not deleted")
	}
	if w = request(h, cookie, "POST", "/api/logout"); w.Code != 204 {
		t.Fatal("logout failed")
	}
	if w = request(h, cookie, "GET", "/api/jobs"); w.Code != 401 {
		t.Fatal("logged out session remained valid")
	}
}

func TestInvalidImagesRejected(t *testing.T) {
	_, h := newTestApp(t)
	cookie := loggedIn(t, h)
	cases := map[string][]byte{"fake": []byte("not a PNG"), "too small": pngImage(t, 299, 300), "bad ratio": pngImage(t, 300, 1000), "too large": bytes.Repeat([]byte("x"), maxImageBytes+1), "truncated": pngImage(t, 360, 640)[:40]}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if w := submit(t, h, cookie, "invalid-123", "商品", data); w.Code != 400 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestMockFailure(t *testing.T) {
	a, h := newTestApp(t)
	a.Provider = &provider.Mock{Delay: time.Nanosecond}
	cookie := loggedIn(t, h)
	j := decodeJob(t, submit(t, h, cookie, "mock-fail-123", "商品 [模拟失败]", pngImage(t, 300, 300)))
	startWorker(t, a)
	j = waitStatus(t, a, j.ID, "failed")
	if !strings.Contains(j.Error, "模拟生成失败") {
		t.Fatal(j.Error)
	}
}

type controlledProvider struct {
	Submits       atomic.Int32
	Downloads     atomic.Int32
	SubmitError   error
	DownloadError bool
	ChecksFail    bool
}

func (p *controlledProvider) Submit(context.Context, provider.Request) (string, error) {
	p.Submits.Add(1)
	return "remote-id", p.SubmitError
}
func (p *controlledProvider) Check(context.Context, string) (provider.Result, error) {
	if p.ChecksFail {
		return provider.Result{}, errors.New("offline")
	}
	return provider.Result{Status: "succeeded", VideoURL: "mock://demo"}, nil
}
func (p *controlledProvider) Download(ctx context.Context, url string) (io.ReadCloser, error) {
	p.Downloads.Add(1)
	if p.DownloadError {
		return nil, errors.New("offline")
	}
	return (&provider.Mock{}).Download(ctx, url)
}

func TestUncertainSubmissionNeverRetried(t *testing.T) {
	a, h := newTestApp(t)
	p := &controlledProvider{SubmitError: errors.New("timeout")}
	a.Provider = p
	j := decodeJob(t, submit(t, h, loggedIn(t, h), "timeout-123", "商品", pngImage(t, 300, 300)))
	startWorker(t, a)
	j = waitStatus(t, a, j.ID, "unknown")
	time.Sleep(20 * time.Millisecond)
	if p.Submits.Load() != 1 || !strings.Contains(j.Error, "勿重复提交") {
		t.Fatal("uncertain submission retried or unsafe message")
	}
}

func TestRecoveryKeepsRemoteIDAndSessions(t *testing.T) {
	a, h := newTestApp(t)
	cookie := loggedIn(t, h)
	data := pngImage(t, 300, 300)
	running := decodeJob(t, submit(t, h, cookie, "running-123", "商品", data))
	running.RemoteID = "remote-id"
	running.Status = "running"
	if err := a.Store.Save(&running); err != nil {
		t.Fatal(err)
	}
	uncertain := decodeJob(t, submit(t, h, cookie, "unknown-123", "商品", data))
	uncertain.Status = "submitting"
	if err := a.Store.Save(&uncertain); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(a.Config, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restarted.Store.Close() })
	p := &controlledProvider{}
	restarted.Provider = p
	if w := request(restarted.Handler(nil), cookie, "GET", "/api/jobs"); w.Code != 200 {
		t.Fatal("session lost on restart")
	}
	got, err := restarted.Store.Get(uncertain.ID)
	if err != nil || got.Status != "unknown" {
		t.Fatalf("submitting recovery: %+v %v", got, err)
	}
	startWorker(t, restarted)
	waitStatus(t, restarted, running.ID, "succeeded")
	if p.Submits.Load() != 0 {
		t.Fatal("restart resubmitted paid task")
	}
}

func TestDownloadFailureDoesNotRegenerate(t *testing.T) {
	a, h := newTestApp(t)
	p := &controlledProvider{DownloadError: true}
	a.Provider = p
	j := decodeJob(t, submit(t, h, loggedIn(t, h), "download-123", "商品", pngImage(t, 300, 300)))
	startWorker(t, a)
	j = waitStatus(t, a, j.ID, "failed")
	if p.Submits.Load() != 1 || p.Downloads.Load() != 3 || !strings.Contains(j.Error, "本地保存连续失败") {
		t.Fatalf("submits %d downloads %d error %s", p.Submits.Load(), p.Downloads.Load(), j.Error)
	}
}

func TestRepeatedQueryFailureIsUnknown(t *testing.T) {
	a, h := newTestApp(t)
	p := &controlledProvider{ChecksFail: true}
	a.Provider = p
	j := decodeJob(t, submit(t, h, loggedIn(t, h), "query-fail-123", "商品", pngImage(t, 300, 300)))
	startWorker(t, a)
	waitStatus(t, a, j.ID, "unknown")
	if p.Submits.Load() != 1 {
		t.Fatal("query failure resubmitted task")
	}
}
