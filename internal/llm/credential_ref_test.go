package llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wii/senv/internal/storage"
)

// TestResolveCredentialMissingRefDiagnostic：档案跨机同步后，credential_ref
// 指向的条目可能尚未在本机——错误须指明完整引用名与修复指引，且保持 fail-closed。
func TestResolveCredentialMissingRefDiagnostic(t *testing.T) {
	pm, _, _ := newTestProviderManager(t)

	// text 引用缺失
	entry := &storage.LLMProviderEntry{Alias: "anthropic", CredentialRef: "text:llm-keys/anthropic"}
	_, err := resolveCredential(entry, pm)
	if err == nil {
		t.Fatal("missing text credential unexpectedly resolved")
	}
	for _, want := range []string{"text:llm-keys/anthropic", "senv text add llm-keys anthropic"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("text missing-ref error %q missing %q", err, want)
		}
	}

	// env 引用缺失
	envEntry := &storage.LLMProviderEntry{Alias: "openai", CredentialRef: "env:llm-env/OPENAI_KEY"}
	_, err = resolveCredential(envEntry, pm)
	if err == nil {
		t.Fatal("missing env credential unexpectedly resolved")
	}
	for _, want := range []string{"env:llm-env/OPENAI_KEY", "senv env set llm-env OPENAI_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("env missing-ref error %q missing %q", err, want)
		}
	}
}

// TestResolveCredentialCorruptRefKeepsDecryptWrap：条目存在但密文损坏时，
// 保留既有 "decrypt credential" 包装（不是缺失场景）。
func TestResolveCredentialCorruptRefKeepsDecryptWrap(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	tm := pm.textManager()
	if err := tm.Set("llm-keys", "corrupt", "real-key"); err != nil {
		t.Fatalf("seed text entry: %v", err)
	}
	// 直接破坏密文文件
	corruptPath := filepath.Join(store.GetDataPath(), "texts", "llm-keys", "corrupt.enc")
	if err := os.WriteFile(corruptPath, []byte("garbage-not-ciphertext"), 0o600); err != nil {
		t.Fatalf("corrupt ciphertext: %v", err)
	}

	entry := &storage.LLMProviderEntry{Alias: "anthropic", CredentialRef: "text:llm-keys/corrupt"}
	_, err := resolveCredential(entry, pm)
	if err == nil {
		t.Fatal("corrupt credential unexpectedly resolved")
	}
	if !strings.Contains(err.Error(), "decrypt credential text:llm-keys/corrupt") {
		t.Errorf("corrupt-ref error = %q, want decrypt credential wrap", err)
	}
	if strings.Contains(err.Error(), "不存在") {
		t.Errorf("corrupt-ref error misclassified as missing: %q", err)
	}
}

// TestSwitchMissingCredentialWritesNothing：凭据引用缺失时 Switch 必须
// fail-closed——返回带引用全名的错误，agent 配置文件零写入。
func TestSwitchMissingCredentialWritesNothing(t *testing.T) {
	pm, store, _ := newTestProviderManager(t)
	// 直接经 storage 写档案，模拟"档案本体从远端同步到本机、凭据条目
	// （text:llm-keys/remote-synced）尚未同步"的真实场景：AddProvider 会
	// 顺带把 key 写进 llm-keys，绕开它。
	now := time.Now()
	if err := store.SaveLLMProvider("remote-synced", &storage.LLMProviderEntry{
		Alias: "remote-synced", BaseURL: "https://api.example.com",
		CredentialRef: "text:llm-keys/remote-synced", Models: []string{"m1"},
		DefaultModel: "m1", CreatedAt: now, UpdatedAt: now,
	}, "test-password"); err != nil {
		t.Fatalf("SaveLLMProvider() error = %v", err)
	}
	home := t.TempDir()
	sm := NewSwitchManager(pm, "", home)
	_, err := sm.Switch("claude-code", "remote-synced", nil, "", "")
	if err == nil {
		t.Fatal("Switch() unexpectedly succeeded without the credential")
	}
	for _, want := range []string{"text:llm-keys/remote-synced", "senv text add llm-keys remote-synced"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".senv-bak") {
			t.Errorf("backup file written despite fail-closed switch: %s", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Errorf("agent config written despite missing credential: %v", err)
	}
}
