package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigValidation(t *testing.T) {
	t.Setenv("APP_PASSWORD", "")
	if _, err := Load(); err == nil {
		t.Fatal("accepted empty password")
	}
	t.Setenv("APP_PASSWORD", "demo")
	t.Setenv("VIDEO_PROVIDER", "seedance")
	t.Setenv("ARK_API_KEY", "")
	t.Setenv("ARK_MODEL_ID", "")
	if _, err := Load(); err == nil {
		t.Fatal("accepted missing Seedance credentials")
	}
	t.Setenv("VIDEO_PROVIDER", "mock")
	t.Setenv("PORT", "8080")
	t.Setenv("COOKIE_SECURE", "false")
	if c, err := Load(); err != nil || c.Provider != "mock" {
		t.Fatalf("mock config: %+v %v", c, err)
	}
}

func TestDotEnvPreservesEnvironmentAndBOM(t *testing.T) {
	const key = "PIC2VIDEO_TEST_DOTENV_VALUE"
	const existing = "PIC2VIDEO_TEST_DOTENV_EXISTING"
	t.Setenv(existing, "from-process")
	_ = os.Unsetenv(key)
	t.Cleanup(func() { os.Unsetenv(key) })
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("\ufeff# 配置\n"+existing+"=from-file\n"+key+"='password # with spaces'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := LoadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(existing) != "from-process" || os.Getenv(key) != "password # with spaces" {
		t.Fatal("dotenv precedence or quoting failed")
	}
}
