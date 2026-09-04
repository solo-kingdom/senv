package session

import (
	"errors"
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
