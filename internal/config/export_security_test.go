package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wii/senv/internal/exportfile"
)

func TestConfigExportPaths(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "source")
	writeFile(t, src, "secret")
	if err := m.Create("app", src, "unused", "", ""); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	t.Chdir(cwd)
	home := t.TempDir()
	t.Setenv("HOME", home)
	absolute := filepath.Join(t.TempDir(), "absolute", "app.conf")

	for _, tt := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "basename", raw: "app.conf", want: filepath.Join(cwd, "app.conf")},
		{name: "relative", raw: filepath.Join("private", "nested", "app.conf"), want: filepath.Join(cwd, "private", "nested", "app.conf")},
		{name: "absolute", raw: absolute, want: absolute},
		{name: "home", raw: "~/configs/app.conf", want: filepath.Join(home, "configs", "app.conf")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := m.Export("app", tt.raw); err != nil {
				t.Fatal(err)
			}
			assertConfigPlainFile(t, tt.want, "secret", 0o600)
		})
	}
	assertConfigDirMode(t, filepath.Join(cwd, "private"), 0o700)
	assertConfigDirMode(t, filepath.Join(cwd, "private", "nested"), 0o700)
}

func TestConfigExportModesAndBackupPermissions(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "source")
	writeFile(t, src, "stored")
	if err := m.Create("app", src, "unused", "", ""); err != nil {
		t.Fatal(err)
	}

	t.Run("new default", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "app.conf")
		if err := m.Export("app", path); err != nil {
			t.Fatal(err)
		}
		assertConfigPlainFile(t, path, "stored", 0o600)
	})

	t.Run("new explicit 0644", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "app.conf")
		if err := m.ExportWithMode("app", path, 0o644); err != nil {
			t.Fatal(err)
		}
		assertConfigPlainFile(t, path, "stored", 0o644)
		other := filepath.Join(t.TempDir(), "default.conf")
		if err := m.Export("app", other); err != nil {
			t.Fatal(err)
		}
		assertConfigPlainFile(t, other, "stored", 0o600)
	})

	for _, tt := range []struct {
		name       string
		existing   fs.FileMode
		requested  fs.FileMode
		wantTarget fs.FileMode
		wantBackup fs.FileMode
	}{
		{name: "default tightens loose target and backup", existing: 0o644, requested: 0o600, wantTarget: 0o600, wantBackup: 0o600},
		{name: "explicit never widens stricter target or backup", existing: 0o400, requested: 0o644, wantTarget: 0o400, wantBackup: 0o400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "app.conf")
			if err := os.WriteFile(target, []byte("local"), tt.existing); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(target, tt.existing); err != nil {
				t.Fatal(err)
			}
			fixed := time.Date(2026, 9, 4, 1, 2, 3, 0, time.UTC)
			oldNow := nowForBackup
			nowForBackup = func() time.Time { return fixed }
			defer func() { nowForBackup = oldNow }()

			if err := m.ExportWithMode("app", target, tt.requested); err != nil {
				t.Fatal(err)
			}
			assertConfigPlainFile(t, target, "stored", tt.wantTarget)
			backup := target + ".senv-backup-" + fixed.Format("20060102150405")
			assertConfigPlainFile(t, backup, "local", tt.wantBackup)
		})
	}
}

func TestConfigInstallModes(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	newTarget := filepath.Join(dir, "new.conf")
	createConfigWithTarget(t, m, "new", "", newTarget, "new")
	plan, err := m.PlanInstall(Scope{Name: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ExecuteInstall(plan); err != nil {
		t.Fatal(err)
	}
	assertConfigPlainFile(t, newTarget, "new", 0o600)

	identical := filepath.Join(dir, "identical.conf")
	if err := os.WriteFile(identical, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	createConfigWithTarget(t, m, "same", "", identical, "same")
	plan, err = m.PlanInstall(Scope{Name: "same"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Items[0].Action != ActionSkip {
		t.Fatalf("plan action = %q, want skip", plan.Items[0].Action)
	}
	if err := m.ExecuteInstall(plan); err != nil {
		t.Fatal(err)
	}
	assertConfigPlainFile(t, identical, "same", 0o600)

	explicit := filepath.Join(dir, "explicit.conf")
	createConfigWithTarget(t, m, "explicit", "", explicit, "shared")
	plan, err = m.PlanInstall(Scope{Name: "explicit"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ExecuteInstallWithMode(plan, 0o644); err != nil {
		t.Fatal(err)
	}
	assertConfigPlainFile(t, explicit, "shared", 0o644)
}

func TestConfigExportAndInstallSymlinkRejection(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "source")
	writeFile(t, src, "stored")
	if err := m.Create("app", src, "unused", "", ""); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("export target symlink", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "target")
		if err := os.Symlink(outside, target); err != nil {
			t.Fatal(err)
		}
		if err := m.Export("app", target); err == nil {
			t.Fatal("export through target symlink succeeded")
		}
	})

	t.Run("export parent symlink", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "linked")
		if err := os.Symlink(filepath.Dir(outside), parent); err != nil {
			t.Fatal(err)
		}
		if err := m.Export("app", filepath.Join(parent, "outside")); err == nil {
			t.Fatal("export through parent symlink succeeded")
		}
	})

	t.Run("install plan target symlink", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "target")
		if err := os.Symlink(outside, target); err != nil {
			t.Fatal(err)
		}
		createConfigWithTarget(t, m, "linked", "", target, "stored")
		plan, err := m.PlanInstall(Scope{Name: "linked"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Items[0].Action != ActionError {
			t.Fatalf("symlink plan action = %q, want error", plan.Items[0].Action)
		}
	})

	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "outside" {
		t.Fatalf("outside changed: data=%q err=%v", data, err)
	}
}

func TestConfigExportBackupSymlinkRejection(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "source")
	writeFile(t, src, "stored")
	if err := m.Create("app", src, "unused", "", ""); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 9, 4, 2, 3, 4, 0, time.UTC)
	oldNow := nowForBackup
	nowForBackup = func() time.Time { return fixed }
	defer func() { nowForBackup = oldNow }()
	backup := target + ".senv-backup-" + fixed.Format("20060102150405")
	if err := os.Symlink(outside, backup); err != nil {
		t.Fatal(err)
	}

	if err := m.Export("app", target); err == nil {
		t.Fatal("export with symlinked backup succeeded")
	}
	assertConfigPlainFile(t, target, "local", 0o600)
	assertConfigPlainFile(t, outside, "outside", 0o600)
}

func TestConfigExportAtomicFailureKeepsOldTarget(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "source")
	writeFile(t, src, "new-complete")
	if err := m.Create("app", src, "unused", "", ""); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("old-complete"), 0o600); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected target atomic failure")
	oldWrite := writePlainFile
	writePlainFile = func(path string, data []byte, mode fs.FileMode) error {
		if path == target {
			return injected
		}
		return exportfile.WriteFile(path, data, mode)
	}
	defer func() { writePlainFile = oldWrite }()

	if err := m.Export("app", target); !errors.Is(err, injected) {
		t.Fatalf("Export error = %v, want injected failure", err)
	}
	assertConfigPlainFile(t, target, "old-complete", 0o600)
	backups, err := filepath.Glob(target + ".senv-backup-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, err=%v", backups, err)
	}
	assertConfigPlainFile(t, backups[0], "old-complete", 0o600)
}

func assertConfigPlainFile(t *testing.T, path, content string, mode fs.FileMode) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(data) != content {
		t.Fatalf("content of %q = %q, want %q", path, data, content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode.Perm() {
		t.Fatalf("mode of %q = %04o, want %04o", path, info.Mode().Perm(), mode.Perm())
	}
}

func assertConfigDirMode(t *testing.T, path string, mode fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode.Perm() {
		t.Fatalf("directory mode of %q = %04o, want %04o", path, info.Mode().Perm(), mode.Perm())
	}
}

func TestConfigExportModeRejectsSpecialBitsBeforeBackup(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "source")
	writeFile(t, src, "stored")
	if err := m.Create("app", src, "unused", "", ""); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.ExportWithMode("app", target, fs.ModeSetuid|0o600); err == nil {
		t.Fatal("special-bit mode was accepted")
	}
	assertConfigPlainFile(t, target, "local", 0o600)
	backups, err := filepath.Glob(target + ".senv-backup-*")
	if err != nil || len(backups) != 0 {
		t.Fatalf("invalid mode created backups: %v, err=%v", backups, err)
	}
}
