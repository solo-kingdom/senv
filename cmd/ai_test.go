package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/llm"
)

// setupAICacheTest 把 configPathFn 隔离到临时目录并返回该目录。
func setupAICacheTest(t *testing.T) string {
	t.Helper()
	cfg := t.TempDir()
	prevCfg := configPathFn
	configPathFn = func() string { return cfg }
	t.Cleanup(func() { configPathFn = prevCfg })
	return cfg
}

// setupAISource 设置 refresh 的 --source 并在测试结束后还原。
func setupAISource(t *testing.T, url string) {
	t.Helper()
	prev := aiRefreshSource
	aiRefreshSource = url
	t.Cleanup(func() { aiRefreshSource = prev })
}

func TestAIRefreshCmdSuccess(t *testing.T) {
	cfg := setupAICacheTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"prov1":{"id":"prov1","name":"P1","models":{"m1":{"id":"m1"},"m2":{"id":"m2"}}}}`))
	}))
	defer srv.Close()
	setupAISource(t, srv.URL)

	var buf bytes.Buffer
	aiRefreshCmd.SetOut(&buf)
	aiRefreshCmd.SetErr(&bytes.Buffer{})
	if err := aiRefreshCmd.RunE(aiRefreshCmd, nil); err != nil {
		t.Fatalf("RunE() error = %v", err)
	}
	if !strings.Contains(buf.String(), "1 个 provider") || !strings.Contains(buf.String(), "2 个 model") {
		t.Fatalf("output = %q, want counts summary", buf.String())
	}
	if _, err := os.Stat(filepath.Join(cfg, "cache", "models-dev.json")); err != nil {
		t.Fatalf("cache file missing: %v", err)
	}
}

func TestAIRefreshCmdFailureKeepsCache(t *testing.T) {
	cfg := setupAICacheTest(t)
	cachePath := filepath.Join(cfg, "cache", "models-dev.json")
	old, err := llm.Parse([]byte(`{"old":{"id":"old","models":{"m":{"id":"m"}}}}`),
		"https://old.test", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := llm.Save(cachePath, old); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	setupAISource(t, srv.URL)

	aiRefreshCmd.SetOut(&bytes.Buffer{})
	aiRefreshCmd.SetErr(&bytes.Buffer{})
	if err := aiRefreshCmd.RunE(aiRefreshCmd, nil); err == nil {
		t.Fatal("RunE() expected error on server failure")
	}
	got, err := llm.Load(cachePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Source != "https://old.test" {
		t.Fatalf("Source = %q, old cache must be kept", got.Source)
	}
}

func TestAICatalogStatusCmd(t *testing.T) {
	cfg := setupAICacheTest(t)
	cachePath := filepath.Join(cfg, "cache", "models-dev.json")

	// 无缓存：提示先 refresh。
	aiCatalogStatusCmd.SetOut(&bytes.Buffer{})
	aiCatalogStatusCmd.SetErr(&bytes.Buffer{})
	err := aiCatalogStatusCmd.RunE(aiCatalogStatusCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "senv ai refresh") {
		t.Fatalf("no-cache error = %v, want refresh hint", err)
	}

	// 缓存损坏：报损坏。
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(cachePath, []byte(`{broken`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	err = aiCatalogStatusCmd.RunE(aiCatalogStatusCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "损坏") {
		t.Fatalf("corrupt error = %v, want corrupt hint", err)
	}

	// 有缓存：展示元信息。
	cat, err := llm.Parse([]byte(`{"p":{"id":"p","models":{"m":{"id":"m"}}}}`),
		"https://x.test", time.Date(2026, 9, 9, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := llm.Save(cachePath, cat); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	var buf bytes.Buffer
	aiCatalogStatusCmd.SetOut(&buf)
	if err := aiCatalogStatusCmd.RunE(aiCatalogStatusCmd, nil); err != nil {
		t.Fatalf("RunE() error = %v", err)
	}
	for _, want := range []string{"拉取时间", "https://x.test", "provider 数：1", "model 数：1"} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("output = %q, want contains %q", buf.String(), want)
		}
	}
}
