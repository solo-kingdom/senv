package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/session"
	"github.com/wii/senv/internal/storage"
)

func TestRunPasswdRequiresCurrentPasswordAndCleansSnapshot(t *testing.T) {
	isolateSessionCache(t)
	cfg, data := newInitializedProject(t, t.TempDir(), "old-secret")
	useProjectPaths(t, cfg, data)

	// Start an active session and leave a stale tui-snapshot.enc on disk.
	sm := session.NewManager(cfg, data)
	defer sm.Close()
	timeout, _ := session.ParseTimeout("8h")
	if err := sm.StartSession("old-secret", timeout); err != nil {
		t.Fatalf("start session: %v", err)
	}
	snapshotPath := filepath.Join(data, "tui-snapshot.enc")
	if err := os.WriteFile(snapshotPath, []byte("old-encrypted-snapshot"), 0o600); err != nil {
		t.Fatalf("seed tui-snapshot.enc: %v", err)
	}

	origPrompt := authPrompt
	t.Cleanup(func() { authPrompt = origPrompt })

	// 1. Even with an active session, a wrong current password must be rejected.
	wrongInputs := []string{"wrong-secret"}
	idx := 0
	authPrompt = func(string) (string, error) {
		v := wrongInputs[idx]
		idx++
		return v, nil
	}
	if err := runPasswd(nil, nil); !errors.Is(err, errInvalidPassword) {
		t.Fatalf("runPasswd with wrong current password = %v, want errInvalidPassword", err)
	}
	if idx != 1 {
		t.Fatalf("prompt count = %d, want 1 (must stop after invalid current password)", idx)
	}
	if _, err := os.Stat(snapshotPath); err != nil {
		t.Fatalf("failed passwd must not remove snapshot: %v", err)
	}

	// 2. Providing the right current password rekeys the vault, clears session,
	//    and removes stale tui-snapshot.enc.
	okInputs := []string{"old-secret", "new-secret", "new-secret"}
	idx = 0
	authPrompt = func(string) (string, error) {
		v := okInputs[idx]
		idx++
		return v, nil
	}
	if err := runPasswd(nil, nil); err != nil {
		t.Fatalf("runPasswd with valid current password failed: %v", err)
	}
	if idx != 3 {
		t.Fatalf("prompt count = %d, want 3 (current + new + confirm)", idx)
	}
	if _, err := os.Stat(snapshotPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale tui-snapshot.enc must be removed after passwd, stat err = %v", err)
	}
	if status := sm.DescribeCache(); status.State != session.StateNoSession {
		t.Fatalf("session must be cleared after passwd, got %s", status.State)
	}
	store := storage.NewManager(cfg, data)
	ok, err := store.VerifyPassword("new-secret")
	if err != nil || !ok {
		t.Fatalf("new password verification failed: ok=%v err=%v", ok, err)
	}
}
