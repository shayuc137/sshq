//go:build windows

package updater

import (
	"fmt"
	"strings"

	"github.com/shayuc137/sshq/internal/powershell"
)

func newPermissionError(stagedPath, targetPath string, err error) *PermissionError {
	copyCommand := fmt.Sprintf("Copy-Item -LiteralPath '%s' -Destination '%s' -Force", psQuote(stagedPath), psQuote(targetPath))
	encoded := powershell.EncodePayload([]byte(copyCommand))
	action := fmt.Sprintf(`$p = Start-Process powershell.exe -Verb RunAs -Wait -PassThru -ArgumentList '-NoProfile','-EncodedCommand','%s'; if ($p.ExitCode -eq 0) { sshq skill update }`, encoded)
	return &PermissionError{TargetPath: targetPath, StagedPath: stagedPath, Action: action, Err: err}
}

func psQuote(value string) string { return strings.ReplaceAll(value, "'", "''") }

func finishOldFile(string) {}

func syncTargetDir(string) error { return nil }
