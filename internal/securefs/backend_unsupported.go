//go:build !linux && !darwin

package securefs

import "io/fs"

func platformOpenRoot(path string) (int, error) {
	return -1, unsupportedPlatformError("open root", path)
}

func platformClose(_ int) error { return ErrUnsupported }

func platformRead(_ int, segments []string) ([]byte, error) {
	return nil, unsupportedPlatformError("read", displayPath(segments))
}

func platformReadWithMode(_ int, segments []string) ([]byte, fs.FileMode, error) {
	return nil, 0, unsupportedPlatformError("read with mode", displayPath(segments))
}

func platformReadDir(_ int, segments []string) ([]DirEntry, error) {
	return nil, unsupportedPlatformError("read directory", displayPath(segments))
}

func platformEnsureDir(_ int, segments []string, _ fs.FileMode, _ bool) error {
	return unsupportedPlatformError("ensure directory", displayPath(segments))
}

func platformMode(_ int, segments []string) (fs.FileMode, error) {
	return 0, unsupportedPlatformError("inspect mode", displayPath(segments))
}

func platformAtomicWrite(_ int, segments []string, _ []byte, _ fs.FileMode, _ *atomicWriteHooks) error {
	return unsupportedPlatformError("atomic write", displayPath(segments))
}

func platformRemove(_ int, segments []string) error {
	return unsupportedPlatformError("remove", displayPath(segments))
}

func platformRename(_ int, oldSegments, newSegments []string) error {
	return unsupportedPlatformError("rename", displayPath(oldSegments)+" -> "+displayPath(newSegments))
}

func platformRemoveTree(_ int, segments []string) error {
	return unsupportedPlatformError("remove tree", displayPath(segments))
}
