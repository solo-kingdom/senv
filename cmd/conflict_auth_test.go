package cmd

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/conflict"
	"github.com/wii/senv/internal/storage"
)

func withConflictAuthVault(t *testing.T, password string) (string, string, []byte) {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	dataDir := filepath.Join(root, "data")
	oldConfig, oldData := configPathFn, dataPath
	configPathFn = func() string { return configDir }
	dataPath = dataDir
	clearAuthMemo()
	t.Cleanup(func() {
		configPathFn = oldConfig
		dataPath = oldData
		clearAuthMemo()
	})

	manager := storage.NewManager(configDir, dataDir)
	if err := manager.Initialize(password); err != nil {
		t.Fatalf("initialize vault: %v", err)
	}
	metadata, err := manager.LoadMetadata()
	if err != nil {
		t.Fatalf("load metadata: %v", err)
	}
	blob, err := storage.ToJSON(metadata)
	if err != nil {
		t.Fatalf("encode metadata: %v", err)
	}
	return configDir, dataDir, blob
}

func TestResolveConflictAuth(t *testing.T) {
	_, _, metadata := withConflictAuthVault(t, "correct-secret")
	originalStdin := stdinIsTerminal
	stdinIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdinIsTerminal = originalStdin })

	prompt := func(string) (string, error) { return "wrong-secret", nil }
	if _, err := resolveConflictAuth(prompt, metadata); !errors.Is(err, errInvalidPassword) {
		t.Fatalf("wrong password error = %v, want errInvalidPassword", err)
	}

	prompt = func(string) (string, error) { return "correct-secret", nil }
	auth, err := resolveConflictAuth(prompt, metadata)
	if err != nil {
		t.Fatalf("valid auth: %v", err)
	}
	if len(auth.Key) == 0 || !auth.RemoteKeyCompatible {
		t.Fatalf("auth = %+v, want key and compatible metadata", auth)
	}

	incompatible := []byte(`{"version":"1.0","salt":"x","password_key":"bad","kdf_iterations":600000}`)
	auth = conflict.NewAuth(auth.Key, incompatible)
	if auth.RemoteKeyCompatible {
		t.Fatal("incompatible metadata must be reported")
	}
}

func TestResolveConflictAuthNeedsSessionWhenNonInteractive(t *testing.T) {
	withConflictAuthVault(t, "correct-secret")
	originalStdin := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = originalStdin })

	_, err := resolveConflictAuth(func(string) (string, error) {
		t.Fatal("prompt must not run in non-interactive mode")
		return "", nil
	}, nil)
	if !errors.Is(err, ErrNeedSession) {
		t.Fatalf("non-interactive auth error = %v, want ErrNeedSession", err)
	}
	if strings.Contains(err.Error(), "correct-secret") || strings.Contains(err.Error(), "passwordKey") {
		t.Fatalf("auth error leaked secret: %v", err)
	}
}
