package storage

import (
	"os"
	"path/filepath"
)

// WriteSensitiveFile writes a file that must stay user-private (metadata,
// settings, config index, session caches). It guarantees the permissions even
// when the file or its directory already exists with looser ones: plain
// os.WriteFile never changes permissions of existing files, so vaults created
// by older versions would keep world-readable settings forever.
//
// Order matters: the existing file is tightened BEFORE overwriting so no
// freshly written content is ever observable under the old loose mode.
func WriteSensitiveFile(path string, data []byte, dirPerm, filePerm os.FileMode) error {
	if err := EnsurePrivateDir(filepath.Dir(path), dirPerm); err != nil {
		return err
	}
	if err := tightenFile(path, filePerm); err != nil {
		return err
	}
	return os.WriteFile(path, data, filePerm)
}

// EnsurePrivateDir creates dir (and parents) with dirPerm and tightens an
// existing looser directory to dirPerm. Use for directories holding
// sensitive content so directories made by older versions converge to 0700.
func EnsurePrivateDir(dir string, dirPerm os.FileMode) error {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&^dirPerm == 0 {
		return nil
	}
	return os.Chmod(dir, dirPerm)
}

// tightenFile restricts a regular file to perm when its current mode is
// looser. Missing files and non-regular files are left alone.
func tightenFile(path string, perm os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&^perm == 0 {
		return nil
	}
	return os.Chmod(path, perm)
}
