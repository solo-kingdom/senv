package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/storage"
)

// newTestManager creates an env manager backed by an initialized temp storage.
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	storeMgr := storage.NewManager(dir+"/cfg", dir+"/data")
	if err := storeMgr.Initialize("test-password"); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return NewManager(storeMgr, "test-password")
}

func TestSetRejectsInvalidEnvKey(t *testing.T) {
	mgr := newTestManager(t)

	invalidKeys := []string{"a/b", "my-key", "1ABC", "", "foo.bar", "colon:name"}
	for _, key := range invalidKeys {
		t.Run("invalid/"+key, func(t *testing.T) {
			if err := mgr.Set("default", key, "v"); err == nil {
				t.Fatalf("Set(%q) = nil, want error", key)
			}
			// Verify nothing was persisted.
			if _, err := mgr.Get("default", key); err == nil {
				t.Errorf("Get(%q) succeeded; invalid key should not be stored", key)
			}
		})
	}
}

func TestSetAcceptsValidEnvKey(t *testing.T) {
	mgr := newTestManager(t)

	valid := []struct{ key, value string }{
		{"API_KEY", "secret"},
		{"_PRIVATE", "v"},
		{"DATABASE_URL", "postgres://db"},
	}
	for _, tc := range valid {
		t.Run("valid/"+tc.key, func(t *testing.T) {
			if err := mgr.Set("default", tc.key, tc.value); err != nil {
				t.Fatalf("Set(%q): %v", tc.key, err)
			}
			got, err := mgr.Get("default", tc.key)
			if err != nil {
				t.Fatalf("Get(%q): %v", tc.key, err)
			}
			if got != tc.value {
				t.Errorf("Get(%q) = %q, want %q", tc.key, got, tc.value)
			}
		})
	}
}

// injectRawEnvVar writes a variable directly into storage, bypassing Set's
// validation. Used to simulate historical invalid keys for Export tolerance.
func injectRawEnvVar(t *testing.T, mgr *Manager, group, key, value string) {
	t.Helper()
	envGroup, err := mgr.loadEnvGroup(group)
	if err != nil {
		envGroup = storage.NewEnvGroup(group)
	}
	if envGroup.Variables == nil {
		envGroup.Variables = make(map[string]string)
	}
	envGroup.Variables[key] = value
	if err := mgr.saveEnvGroup(envGroup); err != nil {
		t.Fatalf("saveEnvGroup: %v", err)
	}
}

// captureStderr swaps os.Stderr with a pipe, runs fn, and returns whatever was
// written to stderr during the call.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

func TestExportRejectsInvalidEnvIdentity(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.Set("default", "API_KEY", "secret"); err != nil {
		t.Fatalf("Set valid: %v", err)
	}
	polluted := filepath.Join(mgr.storage.GetDataPath(), storage.EnvDirName, "default", "openviking")
	if err := os.MkdirAll(polluted, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(polluted, "root_api_key.enc"), []byte("historical"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Export(); err == nil {
		t.Fatal("Export succeeded with an invalid historical env identity")
	}
}

func TestExportNoWarningWhenAllKeysValid(t *testing.T) {
	mgr := newTestManager(t)

	if err := mgr.Set("default", "API_KEY", "secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := mgr.Set("default", "DB_URL", "postgres://db"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var out string
	stderr := captureStderr(t, func() {
		var err error
		out, err = mgr.Export()
		if err != nil {
			t.Fatalf("Export: %v", err)
		}
	})

	if stderr != "" {
		t.Errorf("expected no stderr output, got:\n%s", stderr)
	}
	if !strings.Contains(out, "export API_KEY='secret'") || !strings.Contains(out, "export DB_URL='postgres://db'") {
		t.Errorf("export output missing valid keys, got:\n%s", out)
	}
}
