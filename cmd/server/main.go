package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/Knni9ht/Pic2Video/internal/app"
	"github.com/Knni9ht/Pic2Video/internal/config"
	"github.com/Knni9ht/Pic2Video/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run() error {
	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}
	c, err := config.Load()
	if err != nil {
		return err
	}
	a, err := app.New(c, slog.Default())
	if err != nil {
		return errors.New("无法初始化数据目录或数据库，请检查目录权限与磁盘空间")
	}
	defer a.Store.Close()
	dist, err := web.Dist()
	if err != nil {
		return errors.New("无法读取嵌入的前端资源，请先构建前端")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); a.Run(ctx) }()
	server := &http.Server{
		Addr: net.JoinHostPort(c.Host, c.Port), Handler: a.Handler(dist),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	slog.Info("Pic2Video 已启动", "address", "http://"+server.Addr, "provider", c.Provider)
	select {
	case <-ctx.Done():
	case err = <-serveErr:
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	<-workerDone
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errors.New("HTTP 服务启动失败，请检查监听地址和端口占用")
	}
	return nil
}
