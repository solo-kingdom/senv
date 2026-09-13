package storage_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func newTokenTestManager(t *testing.T) *storage.Manager {
	t.Helper()
	dir := t.TempDir()
	m := storage.NewManager(dir, filepath.Join(dir, "data"))
	if err := m.Initialize("test-password"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return m
}

func TestServerTokenRoundTrip(t *testing.T) {
	m := newTokenTestManager(t)

	if _, err := m.LoadServerToken(); err == nil {
		t.Fatal("LoadServerToken should fail when the file is missing")
	}

	if err := m.SaveServerToken("tok-123"); err != nil {
		t.Fatalf("SaveServerToken: %v", err)
	}
	got, err := m.LoadServerToken()
	if err != nil {
		t.Fatalf("LoadServerToken: %v", err)
	}
	if got != "tok-123" {
		t.Fatalf("token = %q, want %q", got, "tok-123")
	}

	info, err := os.Stat(filepath.Join(m.GetConfigPath(), storage.ServerTokenFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("server-token.json mode = %o, want 0600", perm)
	}
}

func TestSaveServerTokenWritesGitIgnore(t *testing.T) {
	m := newTokenTestManager(t)
	if err := m.SaveServerToken("tok-123"); err != nil {
		t.Fatalf("SaveServerToken: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(m.GetConfigPath(), ".gitignore"))
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}
	for _, name := range []string{"server-token.json", "mcp-exports.json"} {
		if !strings.Contains(string(data), name) {
			t.Fatalf(".gitignore missing entry %q:\n%s", name, data)
		}
	}

	// 重复保存不产生重复条目
	if err := m.SaveServerToken("tok-456"); err != nil {
		t.Fatalf("SaveServerToken second: %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(m.GetConfigPath(), ".gitignore"))
	if got := strings.Count(string(data), "server-token.json"); got != 1 {
		t.Fatalf("server-token.json appears %d times in .gitignore, want 1:\n%s", got, data)
	}
}

func TestEnsureGitIgnorePreservesUserContent(t *testing.T) {
	m := newTokenTestManager(t)
	ignore := filepath.Join(m.GetConfigPath(), ".gitignore")
	if err := os.WriteFile(ignore, []byte("# my rules\n*.log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.EnsureGitIgnoreServerToken(); err != nil {
		t.Fatalf("EnsureGitIgnoreServerToken: %v", err)
	}
	data, err := os.ReadFile(ignore)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{"# my rules", "*.log", "server-token.json"} {
		if !strings.Contains(content, want) {
			t.Fatalf(".gitignore lost user content %q:\n%s", want, content)
		}
	}
}

func TestMigrateServerTokenFromSettings(t *testing.T) {
	m := newTokenTestManager(t)

	// 旧版布局：token 内嵌在 settings.json
	settings, err := m.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Provider = storage.ProviderConfig{
		Type:    "server",
		Address: "https://senv.example.com",
		Token:   "legacy-token",
		Vault:   "main",
	}
	if err := m.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	moved, err := m.MigrateServerTokenFromSettings()
	if err != nil {
		t.Fatalf("MigrateServerTokenFromSettings: %v", err)
	}
	if !moved {
		t.Fatal("expected migration to happen")
	}

	token, err := m.LoadServerToken()
	if err != nil {
		t.Fatalf("LoadServerToken after migration: %v", err)
	}
	if token != "legacy-token" {
		t.Fatalf("token = %q, want legacy-token", token)
	}

	// settings 中的 token 字段必须被清空（该文件会被 git provider 同步）
	after, err := m.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if after.Provider.Token != "" {
		t.Fatalf("settings token not cleared: %q", after.Provider.Token)
	}
	if after.Provider.Address != "https://senv.example.com" || after.Provider.Type != "server" {
		t.Fatalf("non-secret provider fields must survive: %+v", after.Provider)
	}

	// 幂等：再次迁移是 no-op
	moved, err = m.MigrateServerTokenFromSettings()
	if err != nil || moved {
		t.Fatalf("second migration should be a no-op (moved=%v err=%v)", moved, err)
	}
}

func TestClearServerToken(t *testing.T) {
	m := newTokenTestManager(t)
	if err := m.SaveServerToken("tok"); err != nil {
		t.Fatal(err)
	}
	if err := m.ClearServerToken(); err != nil {
		t.Fatalf("ClearServerToken: %v", err)
	}
	if _, err := m.LoadServerToken(); err == nil {
		t.Fatal("token file should be gone")
	}
	// 再次清理是 no-op
	if err := m.ClearServerToken(); err != nil {
		t.Fatalf("ClearServerToken idempotent: %v", err)
	}
}
