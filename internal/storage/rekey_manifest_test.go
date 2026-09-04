package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/securefs"
)

func testRekeyManifest(t *testing.T, manager *Manager) *rekeyManifest {
	t.Helper()
	if err := manager.SaveConfigIndex(NewConfigIndex()); err != nil {
		t.Fatalf("save config index: %v", err)
	}
	manifest, err := manager.newManifest(
		"0123456789abcdef0123456789abcdef",
		[]byte("old metadata"), []byte("new metadata"),
		[]rekeyManifestEntry{{
			Kind:     rekeyManifestEntryEnv,
			Identity: []string{EnvDirName, "default", "API_KEY.enc"},
			OldHash:  contentHash([]byte("old ciphertext")),
			NewHash:  contentHash([]byte("new ciphertext")),
		}},
	)
	if err != nil {
		t.Fatalf("newManifest: %v", err)
	}
	return manifest
}

func TestRekeyManifestRoundTripAndMode(t *testing.T) {
	base := t.TempDir()
	config := filepath.Join(base, "config")
	data := filepath.Join(base, "data")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(config, data)
	want := testRekeyManifest(t, manager)
	if err := manager.saveRekeyManifest(want); err != nil {
		t.Fatalf("saveRekeyManifest: %v", err)
	}
	got, err := manager.loadRekeyManifest()
	if err != nil {
		t.Fatalf("loadRekeyManifest: %v", err)
	}
	if got.TransactionID != want.TransactionID || got.Stage != want.Stage || len(got.Entries) != 1 || got.Entries[0].OldHash != want.Entries[0].OldHash {
		t.Fatalf("round trip mismatch: %#v", got)
	}
	info, err := os.Stat(filepath.Join(config, rekeyManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest mode = %o, want 600", info.Mode().Perm())
	}
}

func TestRekeyManifestRejectsVersionAndInvalidHashes(t *testing.T) {
	base := t.TempDir()
	manager := NewManager(filepath.Join(base, "config"), filepath.Join(base, "data"))
	manifest := testRekeyManifest(t, manager)
	manifest.Version++
	if err := manager.validateManifest(manifest); err == nil {
		t.Fatal("unsupported version accepted")
	}
	manifest.Version = rekeyManifestVersion
	manifest.Entries[0].NewHash = manifest.Entries[0].OldHash
	if err := manager.validateManifest(manifest); err == nil {
		t.Fatal("matching old/new entry hashes accepted")
	}
	manifest.Entries[0].NewHash = "not-a-sha256"
	if err := manager.validateManifest(manifest); err == nil {
		t.Fatal("malformed entry hash accepted")
	}
}

func TestRekeyManifestRejectsCorruptJSON(t *testing.T) {
	base := t.TempDir()
	config := filepath.Join(base, "config")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, rekeyManifestFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(config, filepath.Join(base, "data"))
	if _, err := manager.loadRekeyManifest(); err == nil {
		t.Fatal("corrupt manifest JSON accepted")
	}
}

func TestRekeyManifestRejectsSymlinkTarget(t *testing.T) {
	base := t.TempDir()
	config := filepath.Join(base, "config")
	data := filepath.Join(base, "data")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(base, "sentinel")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(config, data)
	if err := manager.SaveConfigIndex(NewConfigIndex()); err != nil {
		t.Fatal(err)
	}
	manifest, err := manager.newManifest(
		"0123456789abcdef0123456789abcdef", []byte("old metadata"), []byte("new metadata"),
		[]rekeyManifestEntry{{Kind: rekeyManifestEntryEnv, Identity: []string{EnvDirName, "default", "API_KEY.enc"}, OldHash: contentHash([]byte("old ciphertext")), NewHash: contentHash([]byte("new ciphertext"))}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(config, rekeyManifestFile)); err != nil {
		t.Fatal(err)
	}
	err = manager.saveRekeyManifest(manifest)
	if !errors.Is(err, securefs.ErrSymlink) {
		t.Fatalf("save error = %v, want ErrSymlink", err)
	}
	content, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "unchanged" {
		t.Fatalf("symlink target changed: %q", content)
	}
}

func TestRekeyManifestIdentityRejectsControlFiles(t *testing.T) {
	manager, _ := setupTestManager(t)
	manifest, err := manager.newManifest(
		"0123456789abcdef0123456789abcdef", []byte("old metadata"), []byte("new metadata"),
		[]rekeyManifestEntry{{
			Kind:     rekeyManifestEntryEnv,
			Identity: []string{".senv-sync-state.json"},
			OldHash:  contentHash([]byte("old ciphertext")),
			NewHash:  contentHash([]byte("new ciphertext")),
		}},
	)
	if err == nil || manifest != nil {
		t.Fatalf("control-file manifest accepted: manifest=%v err=%v", manifest, err)
	}
}
