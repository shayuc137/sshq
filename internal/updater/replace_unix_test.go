//go:build !windows

package updater

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceBinaryPermissionErrorKeepsVerifiedStage(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sshq")
	staged := filepath.Join(t.TempDir(), "verified sshq")
	if err := os.WriteFile(target, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

	err := replaceBinary(staged, target)
	var permissionErr *PermissionError
	if !errors.As(err, &permissionErr) {
		t.Fatalf("error = %v, want PermissionError", err)
	}
	if !strings.Contains(permissionErr.Action, "sudo install -m 0755") || !strings.Contains(permissionErr.Action, "sshq skill update") {
		t.Fatalf("action = %q", permissionErr.Action)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("verified stage was removed: %v", err)
	}
}
