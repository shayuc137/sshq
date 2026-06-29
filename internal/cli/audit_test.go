package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shayuc137/sshq/internal/appconfig"
	auditpkg "github.com/shayuc137/sshq/internal/audit"
)

func TestAuditCommandPretty(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.log")
	logger, err := auditpkg.NewLogger(appconfig.AuditConfig{Path: auditPath, MaxSize: "1MB"})
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	if err := logger.Record(auditpkg.ExecEntry("ali", "hostname", auditpkg.ResultSuccess, 0, 7, auditpkg.SourceDirect)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	xdg := filepath.Join(dir, "xdg")
	if err := os.MkdirAll(filepath.Join(xdg, "sshq"), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	configBody := "Host ali\n  HostName 192.0.2.10\n  User tester\n"
	sshConfig := filepath.Join(dir, "ssh_config")
	if err := os.WriteFile(sshConfig, []byte(configBody), 0600); err != nil {
		t.Fatalf("WriteFile ssh config: %v", err)
	}
	appConfig := "[audit]\npath = " + quoteTOML(auditPath) + "\n"
	if err := os.WriteFile(filepath.Join(xdg, "sshq", appconfig.FileName), []byte(appConfig), 0600); err != nil {
		t.Fatalf("WriteFile app config: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("SSHQ_CONFIG", sshConfig)

	var out, errOut bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--pretty", "audit", "--last", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("audit command failed: %v\nstderr=%s", err, errOut.String())
	}
	got := out.String()
	if !strings.Contains(got, "direct exec ali success") || !strings.Contains(got, `summary="hostname"`) {
		t.Fatalf("audit output = %q", got)
	}
}

func TestAuditOperationValidation(t *testing.T) {
	cases := []struct {
		op      string
		wantErr bool
	}{
		{"exec", false},
		{"cp", false},
		{"cluster-exec", false},
		{"tunnel-start", false},
		{"tunnel-stop", false},
		{"exc", true},   // typo
		{"bogus", true}, // unknown
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			var out, errOut bytes.Buffer
			cmd := NewRootCommand()
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			cmd.SetArgs([]string{"audit", "--operation", tc.op, "--last", "1"})

			err := cmd.Execute()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("operation %q: expected validation error, got nil", tc.op)
				}
				if !strings.Contains(err.Error(), "invalid --operation") {
					t.Fatalf("operation %q: error = %v, want invalid --operation", tc.op, err)
				}
				return
			}
			// Valid operations must not be rejected by validation. Any error
			// here would have to come from elsewhere (e.g. reading the log),
			// which an empty/default store does not trigger.
			if err != nil && strings.Contains(err.Error(), "invalid --operation") {
				t.Fatalf("operation %q wrongly rejected: %v", tc.op, err)
			}
		})
	}
}

func quoteTOML(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}
