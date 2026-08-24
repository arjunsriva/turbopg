package main

import (
	"strings"
	"testing"
	"time"
)

func TestLoadConfigFailClosed(t *testing.T) {
	t.Setenv("TURBOPG_API_KEY", "")
	t.Setenv("TURBOPG_ALLOW_INSECURE_API_KEY", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected fail-closed API key")
	}
	t.Setenv("TURBOPG_API_KEY", "testapikey")
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected reject default testapikey")
	}
	t.Setenv("TURBOPG_ALLOW_INSECURE_API_KEY", "1")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.effectiveAPIKey() != "testapikey" {
		t.Fatalf("key=%s", cfg.effectiveAPIKey())
	}
}

func TestLoadConfigListen(t *testing.T) {
	t.Setenv("TURBOPG_API_KEY", "secret-key")
	t.Setenv("TURBOPG_LISTEN", "")
	t.Setenv("TURBOPG_PORT", "9090")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:9090" {
		t.Fatalf("listen=%s", cfg.Listen)
	}
	t.Setenv("TURBOPG_LISTEN", "0.0.0.0:8080")
	cfg, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "0.0.0.0:8080" {
		t.Fatalf("listen=%s", cfg.Listen)
	}
}

func TestWithStatementTimeout(t *testing.T) {
	got := withStatementTimeout("postgres://x/db?sslmode=disable", 0)
	if got != "postgres://x/db?sslmode=disable" {
		t.Fatal(got)
	}
	got = withStatementTimeout("postgres://x/db?sslmode=disable", 30*time.Second)
	if !strings.Contains(got, "statement_timeout=30000") {
		t.Fatalf("got=%s", got)
	}
}
