package updater

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type PermissionError struct {
	TargetPath string
	StagedPath string
	Action     string
	Err        error
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("update requires write access to %s: %v", filepath.Dir(e.TargetPath), e.Err)
}

func (e *PermissionError) Unwrap() error { return e.Err }

type RollbackError struct {
	TargetPath string
	OldPath    string
	NewPath    string
	SwapErr    error
	RestoreErr error
}

type replaceOps struct {
	rename func(string, string) error
	remove func(string) error
}

func (e *RollbackError) Error() string {
	return fmt.Sprintf("binary replacement failed and rollback also failed: swap: %v; rollback: %v", e.SwapErr, e.RestoreErr)
}

func replaceBinary(stagedPath, targetPath string) error {
	return replaceBinaryWithOps(stagedPath, targetPath, replaceOps{rename: os.Rename, remove: os.Remove})
}

func replaceBinaryWithOps(stagedPath, targetPath string, ops replaceOps) error {
	info, err := os.Stat(targetPath)
	if err != nil {
		return fmt.Errorf("inspect current executable: %w", err)
	}
	dir := filepath.Dir(targetPath)
	newFile, err := os.CreateTemp(dir, ".sshq-new-*")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return newPermissionError(stagedPath, targetPath, err)
		}
		return fmt.Errorf("create replacement beside current executable: %w", err)
	}
	newPath := newFile.Name()
	keepNew := false
	defer func() {
		if !keepNew {
			_ = ops.remove(newPath)
		}
	}()

	staged, err := os.Open(stagedPath)
	if err != nil {
		newFile.Close()
		return fmt.Errorf("open staged executable: %w", err)
	}
	_, copyErr := io.Copy(newFile, staged)
	closeStagedErr := staged.Close()
	if copyErr != nil {
		newFile.Close()
		return fmt.Errorf("copy staged executable: %w", copyErr)
	}
	if closeStagedErr != nil {
		newFile.Close()
		return fmt.Errorf("close staged executable: %w", closeStagedErr)
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0755
	}
	if err := newFile.Chmod(mode); err != nil {
		newFile.Close()
		return fmt.Errorf("set replacement permissions: %w", err)
	}
	if err := newFile.Sync(); err != nil {
		newFile.Close()
		return fmt.Errorf("sync replacement executable: %w", err)
	}
	if err := newFile.Close(); err != nil {
		return fmt.Errorf("close replacement executable: %w", err)
	}

	oldPath := filepath.Join(dir, "."+filepath.Base(targetPath)+".old")
	if err := ops.remove(oldPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale executable backup: %w", err)
	}
	if err := ops.rename(targetPath, oldPath); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return newPermissionError(stagedPath, targetPath, err)
		}
		return fmt.Errorf("back up current executable: %w", err)
	}
	if err := ops.rename(newPath, targetPath); err != nil {
		if restoreErr := ops.rename(oldPath, targetPath); restoreErr != nil {
			keepNew = true
			return &RollbackError{TargetPath: targetPath, OldPath: oldPath, NewPath: newPath, SwapErr: err, RestoreErr: restoreErr}
		}
		return fmt.Errorf("install replacement executable: %w", err)
	}
	_ = syncTargetDir(dir)
	finishOldFile(oldPath)
	return nil
}
