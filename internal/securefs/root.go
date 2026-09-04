package securefs

import (
	"errors"
	"io"
	"io/fs"
	"sync"
)

// TrustedRoot is an opened directory capability. Implementations must anchor
// every operation to that handle and must never resolve caller-provided paths
// from the process working directory.
type TrustedRoot interface {
	io.Closer
	Read(segments ...string) ([]byte, error)
	ReadDir(segments ...string) ([]DirEntry, error)
	EnsureDir(segments []string, mode fs.FileMode) error
	EnsurePrivateParents(segments []string, mode fs.FileMode) error
	AtomicWrite(segments []string, data []byte, mode fs.FileMode) error
	Remove(segments ...string) error
	Rename(oldSegments, newSegments []string) error
	RemoveTree(segments ...string) error
}

// DirEntry is a validated regular file or directory below a trusted root.
type DirEntry struct {
	Name  string
	IsDir bool
}

// Root anchors operations to an opened directory descriptor. Renaming or
// replacing the pathname used to open it does not redirect later operations.
type Root struct {
	mu          sync.RWMutex
	fd          int
	closed      bool
	closeErr    error
	atomicHooks *atomicWriteHooks
}

// OpenRoot opens path as a trusted directory capability. The root itself must
// be a real directory, not a symbolic link.
func OpenRoot(path string) (*Root, error) {
	fd, err := platformOpenRoot(path)
	if err != nil {
		return nil, err
	}
	return &Root{fd: fd}, nil
}

// Close releases the trusted directory capability.
func (r *Root) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.closeErr
	}
	r.closed = true
	r.closeErr = platformClose(r.fd)
	r.fd = -1
	return r.closeErr
}

// Read reads a regular file without following any symbolic link from root to
// the final component.
func (r *Root) Read(segments ...string) ([]byte, error) {
	if err := validateSegments(segments); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, &PathError{Op: "read", Path: displayPath(segments), Err: ErrRootChanged}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, &PathError{Op: "read", Path: displayPath(segments), Err: ErrRootChanged}
	}
	return platformRead(r.fd, segments)
}

// ReadWithMode reads a regular file and its permissions from the same open
// descriptor, preventing replacement races between content and mode checks.
func (r *Root) ReadWithMode(segments ...string) ([]byte, fs.FileMode, error) {
	if err := validateSegments(segments); err != nil {
		return nil, 0, err
	}
	if r == nil {
		return nil, 0, &PathError{Op: "read with mode", Path: displayPath(segments), Err: ErrRootChanged}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, 0, &PathError{Op: "read with mode", Path: displayPath(segments), Err: ErrRootChanged}
	}
	return platformReadWithMode(r.fd, segments)
}

// ReadDir enumerates a real directory without following symlinks. Every
// returned entry has a portable single-segment identity and is a regular file
// or directory.
func (r *Root) ReadDir(segments ...string) ([]DirEntry, error) {
	for _, segment := range segments {
		if err := ValidateSegment(segment); err != nil {
			return nil, err
		}
	}
	if r == nil {
		return nil, &PathError{Op: "read directory", Path: displayPath(segments), Err: ErrRootChanged}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, &PathError{Op: "read directory", Path: displayPath(segments), Err: ErrRootChanged}
	}
	return platformReadDir(r.fd, segments)
}

// EnsureDir creates missing directories relative to the trusted root without
// following symlinks. Existing intermediate directories are preserved; the
// final directory is tightened to the requested mode without widening it.
func (r *Root) EnsureDir(segments []string, mode fs.FileMode) error {
	return r.ensureDir(segments, mode, true)
}

// EnsurePrivateParents creates missing directories privately without changing
// permissions on existing directories. It is intended for arbitrary export
// paths, where tightening a pre-existing home or working directory is unsafe.
func (r *Root) EnsurePrivateParents(segments []string, mode fs.FileMode) error {
	return r.ensureDir(segments, mode, false)
}

func (r *Root) ensureDir(segments []string, mode fs.FileMode, tightenFinal bool) error {
	if err := validateSegments(segments); err != nil {
		return err
	}
	if mode&^fs.ModePerm != 0 {
		return &PathError{Op: "ensure directory", Path: displayPath(segments), Err: errors.New("mode contains non-permission bits")}
	}
	if r == nil {
		return &PathError{Op: "ensure directory", Path: displayPath(segments), Err: ErrRootChanged}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return &PathError{Op: "ensure directory", Path: displayPath(segments), Err: ErrRootChanged}
	}
	return platformEnsureDir(r.fd, segments, mode, tightenFinal)
}

// Mode returns ordinary permissions for a regular file without following any
// symbolic link from root to the final component.
func (r *Root) Mode(segments ...string) (fs.FileMode, error) {
	if err := validateSegments(segments); err != nil {
		return 0, err
	}
	if r == nil {
		return 0, &PathError{Op: "inspect mode", Path: displayPath(segments), Err: ErrRootChanged}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return 0, &PathError{Op: "inspect mode", Path: displayPath(segments), Err: ErrRootChanged}
	}
	return platformMode(r.fd, segments)
}

// atomicWriteHooks are package-private test seams. Production roots leave them
// nil, so no fault-control surface is exposed outside this package.
type atomicWriteHooks struct {
	write     func(fd int, data []byte) (int, error)
	fileSync  func(fd int) error
	dirSync   func(fd int) error
	rename    func(oldDir int, oldName string, newDir int, newName string) error
	randomHex func() (string, error)
}

// AtomicWrite replaces a regular file from an exclusive temporary file in the
// same trusted directory. Existing permissions are never widened.
func (r *Root) AtomicWrite(segments []string, data []byte, mode fs.FileMode) error {
	if err := validateSegments(segments); err != nil {
		return err
	}
	if mode&^fs.ModePerm != 0 {
		return &PathError{Op: "atomic write", Path: displayPath(segments), Err: errors.New("mode contains non-permission bits")}
	}
	if r == nil {
		return &PathError{Op: "atomic write", Path: displayPath(segments), Err: ErrRootChanged}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return &PathError{Op: "atomic write", Path: displayPath(segments), Err: ErrRootChanged}
	}
	return platformAtomicWrite(r.fd, segments, data, mode, r.atomicHooks)
}

// Remove deletes one regular file relative to the trusted root without
// following a symbolic link.
func (r *Root) Remove(segments ...string) error {
	if err := validateSegments(segments); err != nil {
		return err
	}
	if r == nil {
		return &PathError{Op: "remove", Path: displayPath(segments), Err: ErrRootChanged}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return &PathError{Op: "remove", Path: displayPath(segments), Err: ErrRootChanged}
	}
	return platformRemove(r.fd, segments)
}

// Rename atomically moves an entry between two directories below the same
// trusted root. Neither endpoint nor either parent path may be a symlink.
func (r *Root) Rename(oldSegments, newSegments []string) error {
	if err := validateSegments(oldSegments); err != nil {
		return err
	}
	if err := validateSegments(newSegments); err != nil {
		return err
	}
	if r == nil {
		return &PathError{Op: "rename", Path: displayPath(oldSegments), Err: ErrRootChanged}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return &PathError{Op: "rename", Path: displayPath(oldSegments), Err: ErrRootChanged}
	}
	return platformRename(r.fd, oldSegments, newSegments)
}

// RemoveTree recursively removes a real directory after a complete no-follow
// preflight. Invalid identities or symlinks therefore cause zero deletions.
func (r *Root) RemoveTree(segments ...string) error {
	if err := validateSegments(segments); err != nil {
		return err
	}
	if r == nil {
		return &PathError{Op: "remove tree", Path: displayPath(segments), Err: ErrRootChanged}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return &PathError{Op: "remove tree", Path: displayPath(segments), Err: ErrRootChanged}
	}
	return platformRemoveTree(r.fd, segments)
}

var _ TrustedRoot = (*Root)(nil)
