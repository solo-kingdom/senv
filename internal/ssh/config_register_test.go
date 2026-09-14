package ssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sshConfigForTest(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, ".ssh", "config")
}

func TestRegisterIncludeCreatesMissingConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	changed, err := RegisterInclude()
	if err != nil || !changed {
		t.Fatalf("register = %v, %v", changed, err)
	}
	data, err := os.ReadFile(sshConfigForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != IncludeLine+"\n" {
		t.Fatalf("config = %q", data)
	}
	info, err := os.Stat(sshConfigForTest(t))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, %v", info.Mode().Perm(), err)
	}
}

func TestRegisterIncludePreservesExistingContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := sshConfigForTest(t)
	original := "Host *\n  ServerAliveInterval 30\n\nHost manual\n  HostName 192.0.2.9\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := RegisterInclude()
	if err != nil || !changed {
		t.Fatalf("register = %v, %v", changed, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), IncludeLine+"\n") {
		t.Fatalf("include must be inserted at top:\n%s", data)
	}
	if !strings.Contains(string(data), original) {
		t.Fatalf("user content must be preserved verbatim:\n%s", data)
	}
	bak, err := os.ReadFile(path + ".senv-bak")
	if err != nil || string(bak) != original {
		t.Fatalf("backup = %q, %v", bak, err)
	}
	// 幂等：再次注册不改动文件。
	changed, err = RegisterInclude()
	if err != nil || changed {
		t.Fatalf("second register = %v, %v", changed, err)
	}
	data2, err := os.ReadFile(path)
	if err != nil || string(data2) != string(data) {
		t.Fatalf("config changed on idempotent register:\n%s", data2)
	}
}

func TestUnregisterIncludeRemovesOnlySenvLine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := sshConfigForTest(t)
	original := "Host manual\n  HostName 192.0.2.9\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := UnregisterInclude()
	if err != nil || changed {
		t.Fatalf("unregister without senv line = %v, %v", changed, err)
	}
	if _, err := RegisterInclude(); err != nil {
		t.Fatal(err)
	}
	changed, err = UnregisterInclude()
	if err != nil || !changed {
		t.Fatalf("unregister = %v, %v", changed, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), IncludeLine) {
		t.Fatalf("senv line must be removed:\n%s", data)
	}
	if !strings.Contains(string(data), original) {
		t.Fatalf("user content must survive unregistration:\n%s", data)
	}
	// 幂等：行已不存在。
	changed, err = UnregisterInclude()
	if err != nil || changed {
		t.Fatalf("second unregister = %v, %v", changed, err)
	}
}
