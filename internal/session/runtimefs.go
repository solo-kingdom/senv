package session

import (
	"fmt"
)

type runtimeFilesystemKind uint8

const (
	runtimeFilesystemUnknown runtimeFilesystemKind = iota
	runtimeFilesystemMemory
	runtimeFilesystemDisk
)

var errUnsafeRuntimeFilesystem = fmt.Errorf(
	"%w: runtime filesystem is not a verified memory-backed store",
	ErrNoSecureSessionStore,
)

// runtimeFilesystemProbe is replaceable only by same-package tests. Production
// always uses the build-tagged platform implementation.
var runtimeFilesystemProbe = platformRuntimeFilesystemProbe

func requireMemoryBackedFilesystem(path string) error {
	kind, err := runtimeFilesystemProbe(path)
	if err != nil {
		return fmt.Errorf(
			"%w: cannot inspect %q: %v; set XDG_RUNTIME_DIR to a verified tmpfs/ramfs mount, or rerun with --insecure-cache for headless/CI use",
			errUnsafeRuntimeFilesystem, path, err,
		)
	}
	if kind != runtimeFilesystemMemory {
		return fmt.Errorf(
			"%w: %q is not confirmed as tmpfs/ramfs; set XDG_RUNTIME_DIR to a verified memory-backed mount, or rerun with --insecure-cache for headless/CI use",
			errUnsafeRuntimeFilesystem, path,
		)
	}
	return nil
}
