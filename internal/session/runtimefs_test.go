package session

import (
	"errors"
	"strings"
	"testing"
)

func TestRuntimeFilesystemProbe(t *testing.T) {
	original := runtimeFilesystemProbe
	t.Cleanup(func() { runtimeFilesystemProbe = original })

	queryErr := errors.New("statfs unavailable")
	tests := []struct {
		name string
		kind runtimeFilesystemKind
		err  error
		want bool
	}{
		{name: "memory-backed", kind: runtimeFilesystemMemory},
		{name: "disk-backed", kind: runtimeFilesystemDisk, want: true},
		{name: "unknown", kind: runtimeFilesystemUnknown, want: true},
		{name: "query-error", err: queryErr, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeFilesystemProbe = func(string) (runtimeFilesystemKind, error) {
				return test.kind, test.err
			}
			err := requireMemoryBackedFilesystem("/runtime")
			if (err != nil) != test.want {
				t.Fatalf("requireMemoryBackedFilesystem error = %v, wantError=%v", err, test.want)
			}
			if err != nil && !errors.Is(err, errUnsafeRuntimeFilesystem) {
				t.Fatalf("error = %v, want errUnsafeRuntimeFilesystem", err)
			}
		})
	}
}

func TestUnsafeRuntimeErrorsAreActionable(t *testing.T) {
	original := runtimeFilesystemProbe
	t.Cleanup(func() { runtimeFilesystemProbe = original })

	tests := []struct {
		name      string
		kind      runtimeFilesystemKind
		probeErr  error
		wantProbe string
	}{
		{name: "disk-backed", kind: runtimeFilesystemDisk, wantProbe: "tmpfs/ramfs"},
		{name: "unknown", kind: runtimeFilesystemUnknown, wantProbe: "tmpfs/ramfs"},
		{name: "query-error", kind: runtimeFilesystemUnknown, probeErr: errors.New("statfs unavailable"), wantProbe: "cannot inspect"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeFilesystemProbe = func(string) (runtimeFilesystemKind, error) {
				return test.kind, test.probeErr
			}
			err := requireMemoryBackedFilesystem("/runtime")
			if !errors.Is(err, ErrNoSecureSessionStore) {
				t.Fatalf("error = %v, want ErrNoSecureSessionStore chain", err)
			}
			if !strings.Contains(err.Error(), "--insecure-cache") {
				t.Fatalf("error = %v, missing escape-hatch hint", err)
			}
			if !strings.Contains(err.Error(), test.wantProbe) {
				t.Fatalf("error = %v, missing %q guidance", err, test.wantProbe)
			}
		})
	}
}
