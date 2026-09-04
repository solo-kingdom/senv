package session

import (
	"errors"
	"fmt"
)

type runtimeFilesystemKind uint8

const (
	runtimeFilesystemUnknown runtimeFilesystemKind = iota
	runtimeFilesystemMemory
	runtimeFilesystemDisk
)

var errUnsafeRuntimeFilesystem = errors.New("session cache requires an operating-system-verified memory-backed filesystem")

// runtimeFilesystemProbe is replaceable only by same-package tests. Production
// always uses the build-tagged platform implementation.
var runtimeFilesystemProbe = platformRuntimeFilesystemProbe

func requireMemoryBackedFilesystem(path string) error {
	kind, err := runtimeFilesystemProbe(path)
	if err != nil {
		return fmt.Errorf("%w: cannot inspect %q: %v", errUnsafeRuntimeFilesystem, path, err)
	}
	if kind != runtimeFilesystemMemory {
		return fmt.Errorf("%w: %q is not confirmed as tmpfs/ramfs; set XDG_RUNTIME_DIR to a verified memory-backed mount", errUnsafeRuntimeFilesystem, path)
	}
	return nil
}
