package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/securefs"
)

func TestStorageTextValidation(t *testing.T) {
	m, _ := setupTestManager(t)
	key := derivedKey(t, m, "test-password")
	invalid := []string{"", ".", "..", "../escaped", "a/b", `a\b`, "C:evil", "nul\x00name"}
	for _, group := range invalid {
		t.Run("group/"+group, func(t *testing.T) {
			assertNoStorageAccessForInvalidText(t, m, func() error {
				return m.SaveTextFileWithKey(group, "KEY", NewTextEntry("value"), key)
			})
			assertNoStorageAccessForInvalidText(t, m, func() error {
				_, err := m.LoadTextFileWithKey(group, "KEY", key)
				return err
			})
			assertNoStorageAccessForInvalidText(t, m, func() error { return m.DeleteTextFile(group, "KEY") })
			assertNoStorageAccessForInvalidText(t, m, func() error {
				_, err := m.ListTextFiles(group)
				return err
			})
			assertNoStorageAccessForInvalidText(t, m, func() error { return m.AddTextGroup(group) })
			assertNoStorageAccessForInvalidText(t, m, func() error { return m.DeleteTextGroup(group) })
		})
	}
	for _, textKey := range invalid {
		t.Run("key/"+textKey, func(t *testing.T) {
			assertNoStorageAccessForInvalidText(t, m, func() error {
				return m.SaveTextFileWithKey("notes", textKey, NewTextEntry("value"), key)
			})
			assertNoStorageAccessForInvalidText(t, m, func() error {
				_, err := m.LoadTextFileWithKey("notes", textKey, key)
				return err
			})
			assertNoStorageAccessForInvalidText(t, m, func() error { return m.DeleteTextFile("notes", textKey) })
		})
	}
}

func assertNoStorageAccessForInvalidText(t *testing.T, m *Manager, action func() error) {
	t.Helper()
	original := m.openRoot
	accessed := false
	m.openRoot = func(string) (securefs.TrustedRoot, error) {
		accessed = true
		return nil, fmt.Errorf("unexpected filesystem access")
	}
	defer func() { m.openRoot = original }()
	if err := action(); err == nil {
		t.Fatal("invalid text identity was accepted")
	}
	if accessed {
		t.Fatal("filesystem was accessed before text identity validation")
	}
}

func TestTextTraversalSymlink(t *testing.T) {
	t.Run("parent", func(t *testing.T) {
		m, _ := setupTestManager(t)
		key := derivedKey(t, m, "test-password")
		texts := filepath.Join(m.dataPath, TextDirName)
		outside := t.TempDir()
		sentinel := filepath.Join(outside, "sentinel")
		if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, texts); err != nil {
			t.Fatal(err)
		}
		if err := m.SaveTextFileWithKey("notes", "KEY", NewTextEntry("value"), key); err == nil {
			t.Fatal("SaveTextFileWithKey followed parent symlink")
		}
		assertStorageBytes(t, sentinel, "outside")
		if _, err := os.Lstat(filepath.Join(outside, "notes")); !os.IsNotExist(err) {
			t.Fatalf("outside text group created through symlink: %v", err)
		}
	})

	t.Run("target", func(t *testing.T) {
		m, _ := setupTestManager(t)
		key := derivedKey(t, m, "test-password")
		if err := m.AddTextGroup("notes"); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "sentinel")
		if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(m.dataPath, TextDirName, "notes", "KEY.enc")
		if err := os.Symlink(outside, target); err != nil {
			t.Fatal(err)
		}
		if err := m.SaveTextFileWithKey("notes", "KEY", NewTextEntry("value"), key); err == nil {
			t.Fatal("SaveTextFileWithKey followed target symlink")
		}
		if err := m.DeleteTextFile("notes", "KEY"); err == nil {
			t.Fatal("DeleteTextFile removed target symlink")
		}
		assertStorageBytes(t, outside, "outside")
	})
}

func TestTextDeleteGroupTraversal(t *testing.T) {
	m, _ := setupTestManager(t)
	key := derivedKey(t, m, "test-password")
	if err := m.SaveTextFileWithKey("notes", "KEEP", NewTextEntry("value"), key); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	pollution := filepath.Join(m.dataPath, TextDirName, "notes", "pollution")
	if err := os.Symlink(outside, pollution); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteTextGroup("notes"); err == nil {
		t.Fatal("DeleteTextGroup accepted a nested symlink")
	}
	assertStorageBytes(t, sentinel, "outside")
	if _, err := os.Lstat(filepath.Join(m.dataPath, TextDirName, "notes", "KEEP.enc")); err != nil {
		t.Fatalf("validated group file was deleted before symlink failure: %v", err)
	}
}
