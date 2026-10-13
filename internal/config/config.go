package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Password, Provider, APIKey, ModelID, BaseURL, DataDir, Port, Host string
	CookieSecure                                                      bool
	PollInterval                                                      time.Duration
}

func Load() (Config, error) {
	c := Config{
		Password: os.Getenv("APP_PASSWORD"), Provider: env("VIDEO_PROVIDER", "mock"),
		APIKey: os.Getenv("ARK_API_KEY"), ModelID: os.Getenv("ARK_MODEL_ID"),
		BaseURL: strings.TrimRight(env("ARK_BASE_URL", "https://ark.cn-beijing.volces.com/api/v3"), "/"),
		DataDir: env("DATA_DIR", "./data"), Port: env("PORT", "8080"), Host: env("HOST", "127.0.0.1"), PollInterval: 2 * time.Second,
	}
	if strings.TrimSpace(c.Password) == "" {
		return c, errors.New("必须设置 APP_PASSWORD")
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return c, errors.New("PORT 必须在 1 到 65535 之间")
	}
	if s := os.Getenv("COOKIE_SECURE"); s != "" {
		c.CookieSecure, err = strconv.ParseBool(s)
		if err != nil {
			return c, errors.New("COOKIE_SECURE 必须为 true 或 false")
		}
	}
	switch c.Provider {
	case "mock":
	case "seedance":
		if strings.TrimSpace(c.APIKey) == "" || strings.TrimSpace(c.ModelID) == "" {
			return c, errors.New("Seedance 模式必须设置 ARK_API_KEY 和 ARK_MODEL_ID")
		}
		u, err := url.Parse(c.BaseURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return c, errors.New("ARK_BASE_URL 必须为有效的 HTTPS API 地址")
		}
	default:
		return c, fmt.Errorf("VIDEO_PROVIDER 只支持 mock 或 seedance")
	}
	return c, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// LoadDotEnv loads simple KEY=value assignments without overriding process values.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("无法读取 .env 配置文件")
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	line := 0
	for scanner.Scan() {
		line++
		s := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		s = strings.TrimPrefix(s, "export ")
		key, value, ok := strings.Cut(s, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || key == "" || strings.ContainsAny(key, " \t\x00") {
			return fmt.Errorf(".env 第 %d 行格式无效", line)
		}
		if strings.HasPrefix(value, "\"") {
			unquoted, err := strconv.Unquote(value)
			if err != nil {
				return fmt.Errorf(".env 第 %d 行双引号格式无效", line)
			}
			value = unquoted
		} else if strings.HasPrefix(value, "'") {
			if len(value) < 2 || !strings.HasSuffix(value, "'") {
				return fmt.Errorf(".env 第 %d 行单引号格式无效", line)
			}
			value = value[1 : len(value)-1]
		} else if index := strings.Index(value, " #"); index >= 0 {
			value = strings.TrimSpace(value[:index])
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf(".env 第 %d 行变量名无效", line)
			}
		}
	}
	return scanner.Err()
}
