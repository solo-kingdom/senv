package securefs

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidSegment marks an identity that is not one safe path segment.
	ErrInvalidSegment = errors.New("securefs: invalid path segment")
	// ErrContainment marks a path that cannot be proven to remain below its root.
	ErrContainment = errors.New("securefs: path escapes trusted root")
	// ErrSymlink marks a symbolic link at a protected path component.
	ErrSymlink = errors.New("securefs: symbolic links are not allowed")
	// ErrRootChanged marks a trusted-root identity mismatch.
	ErrRootChanged = errors.New("securefs: trusted root changed")
	// ErrUnsupported marks a platform without a race-safe backend.
	ErrUnsupported = errors.New("securefs: unsupported platform")
	// ErrNotRegular marks a protected file target with an unexpected type.
	ErrNotRegular = errors.New("securefs: target is not a regular file")
)

// PathError identifies a failed operation without exposing file contents.
type PathError struct {
	Op   string
	Path string
	Err  error
}

func (e *PathError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("securefs %s: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("securefs %s %q: %v", e.Op, e.Path, e.Err)
}

func (e *PathError) Unwrap() error { return e.Err }

func unsupportedPlatformError(op, path string) error {
	return &PathError{Op: op, Path: path, Err: ErrUnsupported}
}
