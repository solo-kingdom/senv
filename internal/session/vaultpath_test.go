package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeVaultPathEquivalences(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "vault")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	link := filepath.Join(base, "vault-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink vault: %v", err)
	}
	want, err := normalizeVaultPath(real)
	if err != nil {
		t.Fatalf("normalizeVaultPath(real): %v", err)
	}
	for _, variant := range []string{real, real + "/", real + "/./", real + "/../vault", link, link + "/"} {
		got, err := normalizeVaultPath(variant)
		if err != nil {
			t.Fatalf("normalizeVaultPath(%q): %v", variant, err)
		}
		if got != want {
			t.Fatalf("normalizeVaultPath(%q) = %q, want %q", variant, got, want)
		}
	}
}

func TestNormalizeVaultPathResolvesExistingPrefix(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "vault")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	link := filepath.Join(base, "vault-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink vault: %v", err)
	}
	want, err := normalizeVaultPath(filepath.Join(real, "not-created", "child"))
	if err != nil {
		t.Fatalf("normalizeVaultPath(real child): %v", err)
	}
	got, err := normalizeVaultPath(filepath.Join(link, "not-created", "child"))
	if err != nil {
		t.Fatalf("normalizeVaultPath(link child): %v", err)
	}
	if got != want {
		t.Fatalf("normalizeVaultPath(link child) = %q, want %q", got, want)
	}
}

func TestVaultSlotForEquivalentSpellings(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "vault")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	if got, want := vaultSlotFor(real+"/"), vaultSlotFor(real); got != want {
		t.Fatalf("vaultSlotFor(trailing slash) = %q, want %q", got, want)
	}
	if vaultSlotFor("") == "" {
		t.Fatal("vaultSlotFor must be stable for empty paths")
	}
}
