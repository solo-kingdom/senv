package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRuntimeRootResolvesTrustedSymlink(t *testing.T) {
	base := t.TempDir()
	private := filepath.Join(base, "private")
	if err := os.MkdirAll(filepath.Join(private, "runtime"), 0o700); err != nil {
		t.Fatalf("mkdir private runtime: %v", err)
	}
	link := filepath.Join(base, "var")
	if err := os.Symlink(private, link); err != nil {
		t.Fatalf("symlink runtime: %v", err)
	}
	setRuntimeProbe(t, runtimeFilesystemMemory, nil)

	runtime := filepath.Join(link, "runtime")
	resolved, err := validateRuntimeRoot(runtime)
	if err != nil {
		t.Fatalf("validateRuntimeRoot(%q) error = %v", runtime, err)
	}
	if want := filepath.Join(private, "runtime"); resolved != want {
		t.Fatalf("resolved root = %q, want %q", resolved, want)
	}
}

func TestValidateRuntimeRootRejectsFinalSymlink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "disk-runtime")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	link := filepath.Join(base, "attacker-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink runtime: %v", err)
	}
	setRuntimeProbe(t, runtimeFilesystemMemory, nil)

	if _, err := validateRuntimeRoot(link); err == nil {
		t.Fatal("final runtime symlink was accepted")
	}
}

func TestValidateRuntimeRootRejectsParentSymlinkToDisk(t *testing.T) {
	base := t.TempDir()
	disk := filepath.Join(base, "disk")
	if err := os.MkdirAll(filepath.Join(disk, "runtime"), 0o700); err != nil {
		t.Fatalf("mkdir disk runtime: %v", err)
	}
	link := filepath.Join(base, "var")
	if err := os.Symlink(disk, link); err != nil {
		t.Fatalf("symlink parent: %v", err)
	}
	setRuntimeProbe(t, runtimeFilesystemDisk, nil)

	runtime := filepath.Join(link, "runtime")
	if _, err := validateRuntimeRoot(runtime); !errors.Is(err, errUnsafeRuntimeFilesystem) {
		t.Fatalf("validateRuntimeRoot(%q) error = %v, want unsafe filesystem", runtime, err)
	}
}
