package ssh

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wii/senv/internal/securefs"
	"github.com/wii/senv/internal/storage"
)

func TestSSHValidationRejectsTraversalAndInjection(t *testing.T) {
	if _, err := MaterializePath("..", "k"); err == nil {
		t.Fatal("MaterializePath must reject '..' group")
	}
	if _, err := MaterializePath(".", "k"); err == nil {
		t.Fatal("MaterializePath must reject '.' group")
	}
	if _, err := FragmentPath("bad group"); err == nil {
		t.Fatal("FragmentPath must reject group with space")
	}

	// Extra directives Host/Match/Include must be rejected case-insensitively.
	for _, k := range []string{"Host", "match", "INCLUDE", "Bad Key"} {
		if err := validateExtra(map[string]string{k: "*"}); err == nil {
			t.Fatalf("validateExtra must reject %q", k)
		}
	}
	if err := validateExtra(map[string]string{"ForwardAgent": "yes"}); err != nil {
		t.Fatalf("valid extra rejected: %v", err)
	}

	// Host alias/hostname/user injection guards.
	badHosts := []*storage.HostEntry{
		{Alias: "web*", Hostname: "1.2.3.4"},
		{Alias: "-oProxyCommand", Hostname: "1.2.3.4"},
		{Alias: "web", Hostname: "1.2.3.4 #evil"},
		{Alias: "web", Hostname: "1.2.3.4", User: "root\nHost *"},
	}
	for _, h := range badHosts {
		if err := validateHost(h); err == nil {
			t.Fatalf("validateHost must reject %+v", h)
		}
	}
}

func TestSSHPublicKeySanitizationAndSingleLine(t *testing.T) {
	got := sanitizeOpenSSHComment("alice@host\nssh-ed25519 AAAA evil\x00")
	if got != "alice@host" {
		t.Fatalf("sanitizeOpenSSHComment = %q, want %q", got, "alice@host")
	}
	pubPath := filepath.Join(t.TempDir(), "k")
	if err := writePublicKeyFile(pubPath, "ssh-ed25519 AAAA bad\nssh-ed25519 BBBB"); err == nil {
		t.Fatal("writePublicKeyFile must reject multiline public key")
	}
}

func TestSSHDeletePrunedFilesRejectsOutsideAndSymlinks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	senvDir := filepath.Join(home, ".ssh", "senv")
	if err := os.MkdirAll(filepath.Join(senvDir, "keys", "prod"), 0o700); err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := DeletePrunedFiles([]string{outside}); err == nil {
		t.Fatal("DeletePrunedFiles must reject path outside ~/.ssh/senv")
	}

	linkInKeys := filepath.Join(senvDir, "keys", "prod", "sym")
	if err := os.Symlink(outside, linkInKeys); err != nil {
		t.Fatal(err)
	}
	if _, err := DeletePrunedFiles([]string{linkInKeys}); err == nil {
		t.Fatal("DeletePrunedFiles must reject symlink inside keys/")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside victim missing: %v", err)
	}
}

func TestSSHMaterializedKeyPermissionTighteningAndSymlinkReject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	keyPath, err := MaterializePath("prod", "deploy")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("PRIVATE"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyAndTightenMaterializedKey("prod", "deploy"); err != nil {
		t.Fatalf("verifyAndTightenMaterializedKey: %v", err)
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}

	// Symlink at target must fail closed.
	symPath, _ := MaterializePath("prod", "symkey")
	if err := os.Symlink(keyPath, symPath); err != nil {
		t.Fatal(err)
	}
	if err := verifyAndTightenMaterializedKey("prod", "symkey"); !errors.Is(err, securefs.ErrSymlink) && !strings.Contains(strings.ToLower(err.Error()), "symbolic link") {
		t.Fatalf("expected symlink error, got: %v", err)
	}
}
