//go:build !linux && !darwin

package perflog

import "os"

func openAppendNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
}
