package provider

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

//go:embed demo.mp4
var demoVideo []byte

type Mock struct{ Delay time.Duration }

func (m *Mock) Submit(_ context.Context, r Request) (string, error) {
	fail := "ok"
	if strings.Contains(r.Description, "[模拟失败]") {
		fail = "fail"
	}
	return fmt.Sprintf("%s:%d", fail, time.Now().UnixNano()), nil
}
func (m *Mock) Check(_ context.Context, id string) (Result, error) {
	parts := strings.Split(id, ":")
	if len(parts) != 2 {
		return Result{}, fmt.Errorf("invalid mock task")
	}
	started, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return Result{}, err
	}
	delay := m.Delay
	if delay == 0 {
		delay = 5 * time.Second
	}
	if time.Since(time.Unix(0, started)) < delay {
		return Result{Status: "running"}, nil
	}
	if parts[0] == "fail" {
		return Result{Status: "failed", Error: "模拟生成失败；移除描述中的 [模拟失败] 后可重新生成。"}, nil
	}
	return Result{Status: "succeeded", VideoURL: "mock://demo"}, nil
}
func (m *Mock) Download(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(demoVideo)), nil
}
