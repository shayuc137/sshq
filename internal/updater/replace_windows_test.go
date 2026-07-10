//go:build windows

package updater

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const windowsReplaceHelperEnv = "SSHQ_WINDOWS_REPLACE_HELPER"

func TestWindowsRunningExecutableReplacement(t *testing.T) {
	if os.Getenv(windowsReplaceHelperEnv) == "1" {
		staged := os.Getenv("SSHQ_WINDOWS_REPLACE_STAGED")
		target, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		if err := replaceBinary(staged, target); err != nil {
			t.Fatal(err)
		}
		return
	}

	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "sshq-update-helper.exe")
	copyFile(t, source, target)

	for run := 0; run < 2; run++ {
		staged := filepath.Join(dir, "staged.exe")
		copyFile(t, source, staged)
		cmd := exec.Command(target, "-test.run=^TestWindowsRunningExecutableReplacement$")
		cmd.Env = append(os.Environ(),
			windowsReplaceHelperEnv+"=1",
			"SSHQ_WINDOWS_REPLACE_STAGED="+staged,
		)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("helper run %d failed: %v\n%s", run+1, err, output)
		}
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("replacement target missing after run %d: %v", run+1, err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".sshq-update-helper.exe.old")); err != nil {
			t.Fatalf("old executable missing after run %d: %v", run+1, err)
		}
	}
}

func copyFile(t *testing.T, source, target string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0755); err != nil {
		t.Fatal(err)
	}
}
