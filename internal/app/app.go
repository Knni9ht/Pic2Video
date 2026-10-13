package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Knni9ht/Pic2Video/internal/config"
	"github.com/Knni9ht/Pic2Video/internal/provider"
	"github.com/Knni9ht/Pic2Video/internal/store"
	_ "golang.org/x/image/webp"
)

const maxImageBytes = 10 << 20
const cookieName = "pic2video_session"

type loginAttempt struct {
	Count int
	Until time.Time
}

type App struct {
	Config        config.Config
	Store         *store.Store
	Provider      provider.Provider
	log           *slog.Logger
	createMu      sync.Mutex
	filesMu       sync.RWMutex
	loginMu       sync.Mutex
	loginAttempts map[string]loginAttempt
	work          chan struct{}
}

func New(c config.Config, log *slog.Logger) (*App, error) {
	if log == nil {
		log = slog.Default()
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 2 * time.Second
	}
	for _, dir := range []string{"images", "videos"} {
		if err := os.MkdirAll(filepath.Join(c.DataDir, dir), 0700); err != nil {
			return nil, err
		}
	}
	s, err := store.Open(c.DataDir)
	if err != nil {
		return nil, err
	}
	if err := s.Recover(c.Provider); err != nil {
		s.Close()
		return nil, err
	}
	var p provider.Provider
	switch c.Provider {
	case "mock":
		p = &provider.Mock{}
	case "seedance":
		p = provider.NewSeedance(c.BaseURL, c.APIKey, c.ModelID)
	default:
		s.Close()
		return nil, errors.New("未知视频生成模式")
	}
	a := &App{Config: c, Store: s, Provider: p, log: log, loginAttempts: map[string]loginAttempt{}, work: make(chan struct{}, 1)}
	if err := a.recoverDeletes(); err != nil {
		s.Close()
		return nil, err
	}
	return a, nil
}

func (a *App) Handler(dist fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", a.login)
	mux.HandleFunc("POST /api/logout", a.logout)
	mux.HandleFunc("GET /api/session", a.session)
	mux.Handle("POST /api/jobs", a.auth(http.HandlerFunc(a.createJob)))
	mux.Handle("GET /api/jobs", a.auth(http.HandlerFunc(a.listJobs)))
	mux.Handle("GET /api/jobs/{id}", a.auth(http.HandlerFunc(a.getJob)))
	mux.Handle("DELETE /api/jobs/{id}", a.auth(http.HandlerFunc(a.deleteJob)))
	mux.Handle("GET /api/jobs/{id}/image", a.auth(http.HandlerFunc(a.image)))
	mux.Handle("GET /api/jobs/{id}/video", a.auth(http.HandlerFunc(a.video)))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, 404, "not_found", "接口不存在") })
	if dist != nil {
		files := http.FileServerFS(dist)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				writeError(w, 405, "method_not_allowed", "请求方法不支持")
				return
			}
			files.ServeHTTP(w, r)
		})
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				writeError(w, 405, "method_not_allowed", "请求方法不支持")
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprintln(w, "Pic2Video 后端已启动。请先构建前端，或启动 web 开发服务。")
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			// SameSite cookies and Fetch Metadata prevent cross-site form mutations.
			if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeError(w, 403, "cross_site_request", "不允许跨站操作")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func randomID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (a *App) sessionHash(value string) string { return hash(value + "\x00" + a.Config.Password) }
func (a *App) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(cookieName)
	return err == nil && len(cookie.Value) == 48 && a.Store.HasSession(a.sessionHash(cookie.Value))
}
func (a *App) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authenticated(r) {
			writeError(w, 401, "unauthorized", "请先登录")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *App) session(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"authenticated": a.authenticated(r), "provider": a.Config.Provider})
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	a.loginMu.Lock()
	now := time.Now()
	for key, attempt := range a.loginAttempts {
		if now.After(attempt.Until) {
			delete(a.loginAttempts, key)
		}
	}
	attempt := a.loginAttempts[ip]
	if attempt.Count >= 5 {
		a.loginMu.Unlock()
		w.Header().Set("Retry-After", "300")
		writeError(w, 429, "login_limited", "登录尝试过于频繁，请 5 分钟后重试")
		return
	}
	attempt.Count++
	if attempt.Until.IsZero() {
		attempt.Until = now.Add(5 * time.Minute)
	}
	a.loginAttempts[ip] = attempt
	a.loginMu.Unlock()
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		Password string `json:"password"`
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") || json.NewDecoder(r.Body).Decode(&input) != nil {
		writeError(w, 400, "invalid_request", "请输入有效的登录信息")
		return
	}
	actual := sha256.Sum256([]byte(input.Password))
	expected := sha256.Sum256([]byte(a.Config.Password))
	if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
		writeError(w, 401, "invalid_password", "密码错误")
		return
	}
	token, err := randomID()
	if err != nil {
		writeError(w, 500, "internal_error", "暂时无法登录")
		return
	}
	expires := now.Add(7 * 24 * time.Hour)
	if err := a.Store.AddSession(a.sessionHash(token), expires); err != nil {
		writeError(w, 500, "internal_error", "暂时无法保存会话")
		return
	}
	a.loginMu.Lock()
	delete(a.loginAttempts, ip)
	a.loginMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: a.Config.CookieSecure, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: 7 * 24 * 3600})
	writeJSON(w, 200, map[string]any{"authenticated": true, "provider": a.Config.Provider})
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		_ = a.Store.DeleteSession(a.sessionHash(cookie.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, Secure: a.Config.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) createJob(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 128 || strings.ContainsAny(key, "\r\n\x00") {
		writeError(w, 400, "invalid_idempotency_key", "提交时必须提供 8 至 128 字符的 Idempotency-Key")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes+(64<<10))
	if err := r.ParseMultipartForm(maxImageBytes + (64 << 10)); err != nil {
		writeError(w, 400, "invalid_upload", "上传失败，请使用小于 10 MB 的图片")
		return
	}
	defer r.MultipartForm.RemoveAll()
	description := strings.TrimSpace(r.FormValue("description"))
	if description == "" || !utf8.ValidString(description) || utf8.RuneCountInString(description) > 2000 {
		writeError(w, 400, "invalid_description", "商品描述须为 1 至 2000 个字符")
		return
	}
	duration, err := strconv.Atoi(defaultString(r.FormValue("duration"), "5"))
	ratio := defaultString(r.FormValue("ratio"), "9:16")
	resolution := defaultString(r.FormValue("resolution"), "720p")
	if err != nil || (duration != 5 && duration != 10) || (ratio != "9:16" && ratio != "16:9" && ratio != "1:1") || resolution != "720p" {
		writeError(w, 400, "invalid_parameters", "仅支持 5/10 秒、720p 和 9:16、16:9、1:1 比例")
		return
	}
	if len(r.MultipartForm.File["image"]) != 1 {
		writeError(w, 400, "invalid_image", "请上传一张商品图片")
		return
	}
	f, _, err := r.FormFile("image")
	if err != nil {
		writeError(w, 400, "invalid_image", "请上传一张商品图片")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil || len(data) > maxImageBytes || len(data) == 0 {
		writeError(w, 400, "invalid_image", "图片不可为空且不能超过 10 MB")
		return
	}
	mime, ext, err := validateImage(data)
	if err != nil {
		writeError(w, 400, "invalid_image", err.Error())
		return
	}
	requestHash := hash(fmt.Sprintf("%s\x00%d\x00%s\x00%s\x00%s", description, duration, ratio, resolution, hash(string(data))))
	a.createMu.Lock()
	defer a.createMu.Unlock()
	if existing, err := a.Store.ByKey(key); err == nil {
		if existing.RequestHash != requestHash {
			writeError(w, 409, "idempotency_conflict", "同一个提交标识不能用于不同内容，请重新发起任务")
			return
		}
		writeJSON(w, 202, existing)
		return
	} else if !store.IsNotFound(err) {
		writeError(w, 500, "storage_error", "暂时无法查询任务")
		return
	}
	id, err := randomID()
	if err != nil {
		writeError(w, 500, "internal_error", "暂时无法创建任务")
		return
	}
	imagePath := filepath.Join("images", id+ext)
	fullPath := filepath.Join(a.Config.DataDir, imagePath)
	if err := os.WriteFile(fullPath, data, 0600); err != nil {
		writeError(w, 500, "storage_error", "无法保存图片，请检查服务器磁盘空间")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	j := store.Job{ID: id, Description: description, Duration: duration, Ratio: ratio, Resolution: resolution, Status: "queued", Provider: a.Config.Provider, CreatedAt: now, UpdatedAt: now, ImagePath: imagePath, ImageMIME: mime, IdempotencyKey: key, RequestHash: requestHash, ImageURL: "/api/jobs/" + id + "/image"}
	if err := a.Store.Create(j); err != nil {
		_ = os.Remove(fullPath)
		writeError(w, 500, "storage_error", "无法保存任务")
		return
	}
	select {
	case a.work <- struct{}{}:
	default:
	}
	writeJSON(w, 202, j)
}

func validateImage(data []byte) (string, string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "webp") {
		return "", "", errors.New("仅支持有效的 JPG、PNG 或 WebP 图片")
	}
	if cfg.Width < 300 || cfg.Height < 300 || cfg.Width > 6000 || cfg.Height > 6000 {
		return "", "", errors.New("图片宽高须在 300 至 6000 像素之间")
	}
	ratio := float64(cfg.Width) / float64(cfg.Height)
	if ratio < 0.4 || ratio > 2.5 {
		return "", "", errors.New("图片宽高比须在 0.4 至 2.5 之间")
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return "", "", errors.New("图片内容不完整或已损坏")
	}
	ext := "." + format
	if format == "jpeg" {
		ext = ".jpg"
	}
	return "image/" + format, ext, nil
}
func defaultString(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

func (a *App) listJobs(w http.ResponseWriter, r *http.Request) {
	page, err1 := strconv.Atoi(defaultString(r.URL.Query().Get("page"), "1"))
	size, err2 := strconv.Atoi(defaultString(r.URL.Query().Get("page_size"), "12"))
	if err1 != nil || err2 != nil || page < 1 || page > 1000000 || size < 1 || size > 100 {
		writeError(w, 400, "invalid_pagination", "分页参数无效")
		return
	}
	jobs, total, err := a.Store.List(page, size)
	if err != nil {
		writeError(w, 500, "storage_error", "暂时无法读取任务列表")
		return
	}
	writeJSON(w, 200, map[string]any{"items": jobs, "total": total, "page": page, "page_size": size})
}
func (a *App) lookup(w http.ResponseWriter, r *http.Request) (store.Job, bool) {
	j, err := a.Store.Get(r.PathValue("id"))
	if store.IsNotFound(err) {
		writeError(w, 404, "not_found", "任务不存在")
		return j, false
	}
	if err != nil {
		writeError(w, 500, "storage_error", "暂时无法读取任务")
		return j, false
	}
	return j, true
}
func (a *App) getJob(w http.ResponseWriter, r *http.Request) {
	if j, ok := a.lookup(w, r); ok {
		writeJSON(w, 200, j)
	}
}
func (a *App) deleteJob(w http.ResponseWriter, r *http.Request) {
	a.filesMu.Lock()
	defer a.filesMu.Unlock()
	j, ok := a.lookup(w, r)
	if !ok {
		return
	}
	if j.Status != "succeeded" && j.Status != "failed" && j.Status != "unknown" {
		writeError(w, 409, "job_active", "运行中的任务不能删除")
		return
	}
	if err := a.removeJob(j); err != nil {
		writeError(w, 500, "storage_error", "无法删除任务，请关闭占用的媒体文件后重试")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *App) image(w http.ResponseWriter, r *http.Request) {
	j, ok := a.lookup(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", j.ImageMIME)
	a.serveFile(w, r, j.ImagePath)
}
func (a *App) video(w http.ResponseWriter, r *http.Request) {
	j, ok := a.lookup(w, r)
	if !ok {
		return
	}
	if j.Status != "succeeded" || j.VideoPath == "" {
		writeError(w, 409, "video_not_ready", "视频尚未保存完成")
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="pic2video-`+j.ID+`.mp4"`)
	}
	a.serveFile(w, r, j.VideoPath)
}
func (a *App) serveFile(w http.ResponseWriter, r *http.Request, path string) {
	a.filesMu.RLock()
	defer a.filesMu.RUnlock()
	f, err := os.Open(filepath.Join(a.Config.DataDir, path))
	if err != nil {
		writeError(w, 404, "file_not_found", "文件不存在或不可访问")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, 500, "storage_error", "无法读取文件")
		return
	}
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

// Run processes a single durable queue. Cancel the context, wait for Run, then close Store.
func (a *App) Run(ctx context.Context) {
	for ctx.Err() == nil {
		j, err := a.Store.Next(a.Config.Provider)
		if err == nil {
			a.process(ctx, &j)
			if !pause(ctx, 10*time.Millisecond) {
				return
			}
			continue
		}
		if !store.IsNotFound(err) {
			a.log.Error("读取待处理任务失败")
		}
		select {
		case <-ctx.Done():
			return
		case <-a.work:
		case <-time.After(a.Config.PollInterval):
		}
	}
}
