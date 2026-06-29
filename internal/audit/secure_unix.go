//go:build !windows

package audit

import (
	"fmt"
	"io/fs"
	"os"
)

func setFilePermission(path string, perm fs.FileMode) error {
	if err := os.Chmod(path, perm); err != nil {
		return fmt.Errorf("chmod audit file: %w", err)
	}
	return nil
}

func setDirPermission(path string, perm fs.FileMode) error {
	if err := os.Chmod(path, perm); err != nil {
		return fmt.Errorf("chmod audit directory: %w", err)
	}
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open audit directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync audit directory: %w", err)
	}
	return nil
}
