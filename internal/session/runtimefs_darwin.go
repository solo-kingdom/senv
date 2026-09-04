//go:build darwin

package session

func platformRuntimeFilesystemProbe(string) (runtimeFilesystemKind, error) {
	// Darwin has no implementation in this release that can positively prove
	// the candidate is memory-backed. Unknown media must fail closed.
	return runtimeFilesystemUnknown, nil
}
