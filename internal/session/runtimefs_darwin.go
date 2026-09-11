//go:build darwin

package session

import "golang.org/x/sys/unix"

func platformRuntimeFilesystemProbe(path string) (runtimeFilesystemKind, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return runtimeFilesystemUnknown, err
	}
	if isMemoryBackedFSType(unix.ByteSliceToString(stat.Fstypename[:])) {
		return runtimeFilesystemMemory, nil
	}
	return runtimeFilesystemUnknown, nil
}
