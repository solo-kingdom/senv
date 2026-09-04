//go:build !linux && !darwin

package session

func platformRuntimeFilesystemProbe(string) (runtimeFilesystemKind, error) {
	return runtimeFilesystemUnknown, nil
}
