package provider

import (
	"context"
	"io"
)

type Request struct {
	Description string
	Image       []byte
	ImageMIME   string
	Duration    int
	Ratio       string
	Resolution  string
}

type Result struct {
	Status   string
	VideoURL string
	Error    string
}

type Provider interface {
	Submit(context.Context, Request) (string, error)
	Check(context.Context, string) (Result, error)
	Download(context.Context, string) (io.ReadCloser, error)
}

// SubmissionError distinguishes a definite rejection from an uncertain paid request.
type SubmissionError struct {
	Message  string
	Definite bool
}

func (e *SubmissionError) Error() string { return e.Message }

func Prompt(description string) string {
	return "制作简洁、真实的商品展示短片，以提供的图片作为首帧参考。保持商品的形状、颜色、材质、包装文字和标志一致，不改变商品外观。使用平稳运镜和干净背景展示商品细节。仅呈现描述中明确提供的信息，不编造功效、价格、品牌、认证或其他卖点。不添加字幕、配音或背景音乐。商品描述：\n" + description
}
