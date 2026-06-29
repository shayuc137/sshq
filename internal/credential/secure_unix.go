//go:build !windows

package credential

import (
	"fmt"
	"io/fs"
	"os"
)

func setFilePermission(path string, perm fs.FileMode) error {
	if err := os.Chmod(path, perm); err != nil {
		return fmt.Errorf("chmod credential file: %w", err)
	}
	return nil
}

func setDirPermission(path string, perm fs.FileMode) error {
	if err := os.Chmod(path, perm); err != nil {
		return fmt.Errorf("chmod credential directory: %w", err)
	}
	return nil
}

func checkFilePermission(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat credential file: %w", err)
	}
	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		return fmt.Errorf("credential file has insecure permissions (got %04o)", perm)
	}
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open credential directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync credential directory: %w", err)
	}
	return nil
}
