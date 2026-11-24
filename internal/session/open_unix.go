//go:build linux || darwin

package session

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openAppendNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_APPEND|unix.O_CREAT|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("audit log %s is not a regular file", path)
	}
	return os.NewFile(uintptr(fd), path), nil
}
