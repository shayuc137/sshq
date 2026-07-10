//go:build !windows

package updater

import (
	"fmt"
	"os"
	"strings"
)

func newPermissionError(stagedPath, targetPath string, err error) *PermissionError {
	action := fmt.Sprintf("sudo install -m 0755 %s %s && sshq skill update", shellQuote(stagedPath), shellQuote(targetPath))
	return &PermissionError{TargetPath: targetPath, StagedPath: stagedPath, Action: action, Err: err}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func finishOldFile(path string) { _ = os.Remove(path) }

func syncTargetDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
