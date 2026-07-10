package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/spf13/cobra"
)

func TestShellForExec(t *testing.T) {
	profile := &remote.Profile{Shell: remote.PowerShell}
	if got := shellForExec(profile, "bash"); got != "bash" {
		t.Errorf("override shell = %q, want bash", got)
	}
	if got := shellForExec(profile, ""); got != "powershell" {
		t.Errorf("profile shell = %q, want powershell", got)
	}
	if got := shellForExec(nil, ""); got != "" {
		t.Errorf("empty shell = %q, want empty", got)
	}
}

func TestRootShortcutRequiresConfiguredAlias(t *testing.T) {
	cmd, _, _ := rootCommandForTest(t, "rn")
	cmd.SetArgs([]string{"rn"})

	err := cmd.Execute()
	var cmdErr *output.CmdError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("expected output.CmdError, got %v", err)
	}
	if !strings.Contains(cmdErr.Hint, "command required") {
		t.Fatalf("hint = %q, want command required", cmdErr.Hint)
	}
}

func TestRootCommandPrefersExistingSubcommand(t *testing.T) {
	cmd, out, _ := rootCommandForTest(t, "version")
	cmd.SetArgs([]string{"--pretty", "version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}
	if got := out.String(); !strings.HasPrefix(got, "sshq ") {
		t.Fatalf("version stdout = %q, want sshq prefix", got)
	}
}

func TestVersionJSONEnvelope(t *testing.T) {
	cmd, out, _ := rootCommandForTest(t)
	cmd.SetArgs([]string{"--json", "version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Version string `json:"version"`
			Commit  string `json:"commit"`
			Date    string `json:"date"`
		} `json:"data"`
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if !env.OK || env.SchemaVersion != output.SchemaVersion {
		t.Fatalf("envelope = %+v", env)
	}
	if env.Data.Version == "" {
		t.Fatalf("version data = %+v", env.Data)
	}
}

func rootCommandForTest(t *testing.T, aliases ...string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	var cfg strings.Builder
	for _, alias := range aliases {
		cfg.WriteString("Host " + alias + "\n")
		cfg.WriteString("  HostName 192.0.2.10\n")
		cfg.WriteString("  User tester\n")
	}
	if err := os.WriteFile(cfgPath, []byte(cfg.String()), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSHQ_CONFIG", cfgPath)
	t.Setenv(remote.CachePathEnv, filepath.Join(dir, "profiles.json"))

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	return cmd, out, errOut
}
