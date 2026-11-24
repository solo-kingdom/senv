package tree

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/storage"
)

func setupTreeTest(t *testing.T) (*Manager, *storage.Manager) {
	t.Helper()
	tmp := t.TempDir()
	store := storage.NewManager(filepath.Join(tmp, "config"), filepath.Join(tmp, "data"))
	if err := store.Initialize("test-password"); err != nil {
		t.Fatal(err)
	}
	key, err := store.DeriveKeyFromPassword("test-password")
	if err != nil {
		t.Fatal(err)
	}
	return NewManager(store, key), store
}

func TestTreeSetGetListRemoveAndSyncDir(t *testing.T) {
	tm, _ := setupTreeTest(t)
	if err := tm.AddGroup("deploy", "Deployment trees"); err != nil {
		t.Fatalf("AddGroup: %v", err)
	}

	// Build a local source dir
	srcDir := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(filepath.Join(srcDir, "caddy", "configs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "caddy", "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "caddy", "configs", "run.sh"), []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Ignored by default
	if err := os.WriteFile(filepath.Join(srcDir, "caddy", ".env"), []byte("SECRET=123\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := tm.Push(srcDir, "deploy", "tcbj", false, false, false)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(plan) != 2 {
		t.Fatalf("expected 2 pushed files (ignoring .env), got %d: %+v", len(plan), plan)
	}

	// Second push should be no-op
	plan2, err := tm.Push(srcDir, "deploy", "tcbj", false, false, false)
	if err != nil {
		t.Fatalf("Push 2: %v", err)
	}
	if len(plan2) != 0 {
		t.Fatalf("expected 0 changes on second push, got %+v", plan2)
	}

	// Pull to a fresh dir and verify +x bit preserved
	destDir := filepath.Join(t.TempDir(), "dest")
	pullPlan, err := tm.Pull("deploy", "tcbj", destDir, false, false)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(pullPlan) != 2 {
		t.Fatalf("expected 2 pulled files, got %d", len(pullPlan))
	}
	st, err := os.Stat(filepath.Join(destDir, "caddy", "configs", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected executable bit preserved, got %v", st.Mode().Perm())
	}

	// Diff should be clean
	diffs, err := tm.Diff("deploy", "tcbj", destDir)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(diffs) != 0 {
		t.Fatalf("expected 0 diffs, got %+v", diffs)
	}
}
