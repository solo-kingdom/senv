package tree

import (
	"bytes"
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

func TestListMatchesGet(t *testing.T) {
	tm, _ := setupTreeTest(t)
	if err := tm.AddGroup("deploy", "trees"); err != nil {
		t.Fatal(err)
	}
	files := []struct {
		path string
		raw  []byte
		mode uint32
		desc string
	}{
		{"notes/readme.txt", []byte("hello\n"), 0o644, "note"},
		{"bin/blob.dat", []byte{0xff, 0xfe, 0x00, 0x10}, 0o600, "blob"},
		{"script/run.sh", []byte("#!/bin/sh\necho ok\n"), 0o755, "run"},
	}
	for _, f := range files {
		if _, err := tm.Set("deploy", f.path, f.raw, f.mode, f.desc, false); err != nil {
			t.Fatalf("set %s: %v", f.path, err)
		}
	}

	metas, err := tm.List("deploy", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != len(files) {
		t.Fatalf("list = %d, want %d", len(metas), len(files))
	}
	byPath := map[string]Meta{}
	for _, mt := range metas {
		byPath[mt.Path] = mt
	}
	for _, f := range files {
		mt, ok := byPath[f.path]
		if !ok {
			t.Fatalf("list missing %s", f.path)
		}
		got, err := tm.Get("deploy", f.path)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := got.DecodeBytes()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(raw, f.raw) {
			t.Fatalf("%s content changed", f.path)
		}
		if mt.Mode != got.Mode || mt.Size != len(raw) || mt.SHA256 != got.SHA256 || mt.Description != got.Description || !mt.UpdatedAt.Equal(got.UpdatedAt) {
			t.Fatalf("%s meta list=%+v get mode=%o size=%d sha=%s desc=%q updated=%s",
				f.path, mt, got.Mode, len(raw), got.SHA256, got.Description, got.UpdatedAt)
		}
	}

	prefixed, err := tm.List("deploy", "bin")
	if err != nil {
		t.Fatal(err)
	}
	if len(prefixed) != 1 || prefixed[0].Path != "bin/blob.dat" {
		t.Fatalf("prefix list = %+v", prefixed)
	}
}
