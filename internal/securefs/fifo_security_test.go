//go:build linux || darwin

package securefs

import (
	"errors"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReadWithModeRejectsFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	root, err := OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	_, _, err = root.ReadWithMode("pipe")
	if !errors.Is(err, ErrNotRegular) {
		t.Fatalf("expected ErrNotRegular on FIFO, got: %v", err)
	}
}
