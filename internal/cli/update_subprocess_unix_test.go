//go:build !windows

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRefreshSkillsWithNewBinaryParsesChildEnvelope(t *testing.T) {
	script := filepath.Join(t.TempDir(), "sshq")
	content := `#!/bin/sh
printf '%s\n' '{"ok":true,"data":{"current_version":"0.4.0","updates":[{"target":"claude","scope":"user","path":"/tmp/skill","from_version":"0.3.0","to_version":"0.4.0","updated":true}]},"schema_version":2}'
`
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	result, warnings, err := refreshSkillsWithNewBinary(context.Background(), script)
	if err != nil {
		t.Fatal(err)
	}
	if warnings != "" || len(result.Updates) != 1 || !result.Updates[0].Updated || result.Updates[0].ToVersion != "0.4.0" {
		t.Fatalf("result=%+v warnings=%q", result, warnings)
	}
}
