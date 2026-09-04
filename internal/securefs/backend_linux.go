//go:build linux

package securefs

import (
	"errors"

	"golang.org/x/sys/unix"
)

func platformOpenRead(rootFD int, segments []string) (int, error) {
	how := &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	}
	fd, err := unix.Openat2(rootFD, displayPath(segments), how)
	if err == nil {
		return fd, nil
	}
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOTDIR) {
		return openReadFallback(rootFD, segments)
	}
	return -1, syscallPathError("open", displayPath(segments), err)
}

func platformOpenParent(rootFD int, segments []string) (int, error) {
	if len(segments) == 0 {
		return openParentFallback(rootFD, nil)
	}
	how := &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	}
	fd, err := unix.Openat2(rootFD, displayPath(segments), how)
	if err == nil {
		return fd, nil
	}
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOTDIR) {
		return openParentFallback(rootFD, segments)
	}
	return -1, syscallPathError("open directory", displayPath(segments), err)
}
