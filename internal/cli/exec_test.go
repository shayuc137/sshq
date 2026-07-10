package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/ipc"
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

func TestAppendPowerShellVariableHint(t *testing.T) {
	tests := []struct {
		name     string
		stderr   string
		shell    string
		exitCode int
		wantHint bool
	}{
		{name: "parser error", stderr: "ParserError: unexpected token\n", shell: "powershell", exitCode: 1, wantHint: true},
		{name: "terminator error via pwsh", stderr: "FullyQualifiedErrorId: TerminatorExpectedAtEndOfString", shell: "pwsh.exe", exitCode: 1, wantHint: true},
		{name: "strict variable error", stderr: "Variable is not set.", shell: "powershell", exitCode: 1, wantHint: true},
		{name: "successful command", stderr: "ParserError appears in user output", shell: "powershell", exitCode: 0},
		{name: "different shell", stderr: "ParserError: unexpected token", shell: "bash", exitCode: 1},
		{name: "unrelated error", stderr: "Access is denied", shell: "powershell", exitCode: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendPowerShellVariableHint(tt.stderr, tt.shell, tt.exitCode)
			if hasHint := strings.Contains(got, powerShellVariableHint); hasHint != tt.wantHint {
				t.Fatalf("stderr = %q, hint=%v, want %v", got, hasHint, tt.wantHint)
			}
			if !strings.HasPrefix(got, tt.stderr) {
				t.Fatalf("original stderr changed: got %q, want prefix %q", got, tt.stderr)
			}
		})
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

func TestRootShortcutSupportsExecFlags(t *testing.T) {
	missingScript := filepath.Join(t.TempDir(), "missing.sh")
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "canonical",
			args: []string{"exec", "rn", "echo ok", "--script-file", missingScript, "--shell", "bash", "--no-daemon"},
		},
		{
			name: "shortcut",
			args: []string{"rn", "echo ok", "--script-file", missingScript, "--shell", "bash", "--no-daemon"},
		},
	}

	var wantHint string
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, _, _ := rootCommandForTest(t, "rn")
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			var cmdErr *output.CmdError
			if !errors.As(err, &cmdErr) {
				t.Fatalf("expected output.CmdError, got %v", err)
			}
			if !strings.Contains(cmdErr.Hint, "read script file") {
				t.Fatalf("hint = %q, want read script file", cmdErr.Hint)
			}
			if wantHint == "" {
				wantHint = cmdErr.Hint
			} else if cmdErr.Hint != wantHint {
				t.Fatalf("shortcut hint = %q, want canonical hint %q", cmdErr.Hint, wantHint)
			}
		})
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

func TestRecvExecFramesJSONEnvelope(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		wantErr  bool
		wantCode int
	}{
		{name: "success", code: 0, wantCode: 0},
		{name: "remote non-zero", code: 3, wantErr: true, wantCode: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientConn, serverConn := net.Pipe()
			clientConn.SetDeadline(time.Now().Add(5 * time.Second))
			go func() {
				defer serverConn.Close()
				_ = ipc.Send(serverConn, ipc.Frame{Type: "stdout", Data: "hello\n"})
				_ = ipc.Send(serverConn, ipc.Frame{Type: "exit", Code: tt.code})
			}()

			out := &bytes.Buffer{}
			w := output.New(out, &bytes.Buffer{}, output.WithJSON())
			err := recvExecFrames(w, clientConn, "rn")
			clientConn.Close()

			var exitErr *exec.ExitError
			if tt.wantErr != errors.As(err, &exitErr) {
				t.Fatalf("error = %v, want remote exit error=%v", err, tt.wantErr)
			}

			var env struct {
				OK       bool `json:"ok"`
				ExitCode int  `json:"exit_code"`
				Data     struct {
					ExitCode int    `json:"exit_code"`
					Stdout   string `json:"stdout"`
				} `json:"data"`
			}
			if err := json.Unmarshal(out.Bytes(), &env); err != nil {
				t.Fatalf("invalid JSON: %v\n%s", err, out.String())
			}
			if !env.OK || env.ExitCode != tt.wantCode || env.Data.ExitCode != tt.wantCode {
				t.Fatalf("envelope = %+v, want ok=true and exit_code=%d", env, tt.wantCode)
			}
			if env.Data.Stdout != "hello\n" {
				t.Fatalf("data.stdout = %q, want hello", env.Data.Stdout)
			}
		})
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
