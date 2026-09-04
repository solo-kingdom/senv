//go:build linux

package session

import "golang.org/x/sys/unix"

const (
	linuxTmpfsMagic int64 = 0x01021994
	linuxRamfsMagic int64 = 0x858458f6
)

func platformRuntimeFilesystemProbe(path string) (runtimeFilesystemKind, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return runtimeFilesystemUnknown, err
	}
	switch int64(stat.Type) {
	case linuxTmpfsMagic, linuxRamfsMagic:
		return runtimeFilesystemMemory, nil
	default:
		// A filesystem is unsafe unless it is positively identified above.
		return runtimeFilesystemUnknown, nil
	}
}
