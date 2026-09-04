package storage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/wii/senv/internal/securefs"
)

var errInjectedAtomicWrite = errors.New("injected storage atomic interruption")

type failingAtomicRoot struct{ securefs.TrustedRoot }

func (f *failingAtomicRoot) AtomicWrite(_ []string, _ []byte, _ fs.FileMode) error {
	return errInjectedAtomicWrite
}

func injectAtomicFailure(m *Manager) {
	m.openRoot = func(path string) (securefs.TrustedRoot, error) {
		root, err := securefs.OpenRoot(path)
		if err != nil {
			return nil, err
		}
		return &failingAtomicRoot{TrustedRoot: root}, nil
	}
}

func TestMetadataSymlink(t *testing.T) {
	testConfigWriteSymlinks(t, MetadataFile, func(m *Manager) error {
		return m.SaveMetadata(NewMetadata("salt", "key"))
	})
}

func TestSettingsSymlink(t *testing.T) {
	testConfigWriteSymlinks(t, SettingsFile, func(m *Manager) error {
		return m.SaveSettings(NewSettings())
	})
}

func TestConfigIndexSymlink(t *testing.T) {
	testConfigWriteSymlinks(t, ConfigIndexFile, func(m *Manager) error {
		return m.SaveConfigIndex(NewConfigIndex())
	})
}

func testConfigWriteSymlinks(t *testing.T, filename string, save func(*Manager) error) {
	t.Helper()
	t.Run("target", func(t *testing.T) {
		m, _ := setupTestManager(t)
		outside := filepath.Join(t.TempDir(), "sentinel")
		if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(m.configPath, filename)
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, target); err != nil {
			t.Fatal(err)
		}
		if err := save(m); err == nil {
			t.Fatal("save through target symlink succeeded")
		}
		assertStorageBytes(t, outside, "outside")
	})

	t.Run("parent", func(t *testing.T) {
		m, _ := setupTestManager(t)
		realConfig := m.configPath + "-real"
		if err := os.Rename(m.configPath, realConfig); err != nil {
			t.Fatal(err)
		}
		outsideDir := t.TempDir()
		outside := filepath.Join(outsideDir, filename)
		if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outsideDir, m.configPath); err != nil {
			t.Fatal(err)
		}
		if err := save(m); err == nil {
			t.Fatal("save through parent symlink succeeded")
		}
		assertStorageBytes(t, outside, "outside")
	})
}

func TestMetadataPermission(t *testing.T) {
	testConfigWritePermissions(t, MetadataFile, func(m *Manager) error {
		metadata, err := m.LoadMetadata()
		if err != nil {
			return err
		}
		return m.SaveMetadata(metadata)
	})
}

func TestSettingsPermission(t *testing.T) {
	testConfigWritePermissions(t, SettingsFile, func(m *Manager) error {
		settings, err := m.LoadSettings()
		if err != nil {
			return err
		}
		return m.SaveSettings(settings)
	})
}

func TestConfigIndexPermission(t *testing.T) {
	testConfigWritePermissions(t, ConfigIndexFile, func(m *Manager) error {
		index, err := m.LoadConfigIndex()
		if err != nil {
			return err
		}
		return m.SaveConfigIndex(index)
	})
}

func testConfigWritePermissions(t *testing.T, filename string, save func(*Manager) error) {
	t.Helper()
	m, _ := setupTestManager(t)
	path := filepath.Join(m.configPath, filename)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(m.configPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := save(m); err != nil {
		t.Fatalf("save: %v", err)
	}
	assertStorageMode(t, path, 0o600)
	assertStorageMode(t, m.configPath, 0o700)
}

func TestMetadataAtomicInterruption(t *testing.T) {
	testConfigAtomicInterruption(t, MetadataFile, func(m *Manager) error {
		return m.SaveMetadata(NewMetadata("changed", "changed"))
	})
}

func TestSettingsAtomicInterruption(t *testing.T) {
	testConfigAtomicInterruption(t, SettingsFile, func(m *Manager) error {
		settings := NewSettings()
		settings.Session.Timeout = "never"
		return m.SaveSettings(settings)
	})
}

func TestConfigIndexAtomicInterruption(t *testing.T) {
	testConfigAtomicInterruption(t, ConfigIndexFile, func(m *Manager) error {
		index := NewConfigIndex()
		index.Configs["new"] = ConfigFile{Name: "new", EncryptedFile: "new.enc", Group: ConfigDefaultGroup}
		return m.SaveConfigIndex(index)
	})
}

func testConfigAtomicInterruption(t *testing.T, filename string, save func(*Manager) error) {
	t.Helper()
	m, _ := setupTestManager(t)
	path := filepath.Join(m.configPath, filename)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	injectAtomicFailure(m)
	if err := save(m); !errors.Is(err, errInjectedAtomicWrite) {
		t.Fatalf("save error = %v, want injected interruption", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("target changed after interrupted write")
	}
}

func assertStorageBytes(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}

func assertStorageMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("mode(%s) = %04o, want %04o", path, info.Mode().Perm(), want)
	}
}
