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

func TestMiniModelDefaultAndOverride(t *testing.T) {
	t.Setenv("APP_PASSWORD", "fixture-password")
	t.Setenv("VIDEO_PROVIDER", "seedance")
	t.Setenv("ARK_API_KEY", "fixture-key")
	t.Setenv("ARK_MODEL_ID", "")
	t.Setenv("ARK_BASE_URL", "https://ark.cn-beijing.volces.com/api/v3")
	t.Setenv("PORT", "8080")
	t.Setenv("COOKIE_SECURE", "false")
	c, err := Load()
	if err != nil || c.ModelID != DefaultModelID {
		t.Fatalf("mini default: %s %v", c.ModelID, err)
	}
	t.Setenv("ARK_MODEL_ID", "account-specific-endpoint")
	c, err = Load()
	if err != nil || c.ModelID != "account-specific-endpoint" {
		t.Fatalf("explicit model was ignored: %s %v", c.ModelID, err)
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
