package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/credential"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/sshclient"
)

type testNetError struct{}

func (testNetError) Error() string   { return "network unavailable" }
func (testNetError) Timeout() bool   { return true }
func (testNetError) Temporary() bool { return true }

// TestHostToConnConfigCredentialErrorSurfaced verifies M2: a real
// credential-store error (here a corrupt file) is returned instead of being
// swallowed into a passwordless config that would later look like a generic
// auth failure.
func TestHostToConnConfigCredentialErrorSurfaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.age")
	if err := os.WriteFile(path, []byte("this is not an age file"), 0600); err != nil {
		t.Fatal(err)
	}
	creds, err := credential.Open(credential.WithPath(path))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	host := config.Host{Alias: "prod", HostName: "192.0.2.10", Port: "22", User: "tester"}
	_, err = hostToConnConfigWithCredentials(host, nil, creds)
	if err == nil {
		t.Fatal("expected credential error to be surfaced, got nil")
	}
	if errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("error should not be ErrNotFound: %v", err)
	}

	// The mapped summary must stay password-free and human-readable.
	summary := credentialErrorSummary(err)
	if summary == "" {
		t.Fatal("credentialErrorSummary returned empty string")
	}
	if strings.Contains(summary, "age file") {
		// sanity: summary should be a classification, not raw file bytes
		t.Fatalf("unexpected summary leaking input: %q", summary)
	}
}

func TestConnErrorToOutputHostKeyDetails(t *testing.T) {
	for _, tt := range []struct {
		name    string
		kind    sshclient.ConnErrorKind
		code    string
		action  string
		knownFP string
	}{
		{name: "unknown", kind: sshclient.ErrHostKeyUnknown, code: "host_key_unknown", action: "run: sshq trust target"},
		{name: "mismatch", kind: sshclient.ErrHostKeyMismatch, code: "host_key_mismatch", action: "run: sshq trust target --replace", knownFP: "SHA256:known"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmdErr := connErrorToOutput(&sshclient.ConnError{
				Kind: tt.kind, Alias: "target", Host: "192.0.2.10", Port: "2222", ProxyJump: "jump",
				LookupKeys:        []string{"target", "192.0.2.10", "[192.0.2.10]:2222"},
				RemoteFingerprint: "SHA256:remote", KnownFingerprint: tt.knownFP,
			}, "outer")
			if cmdErr.Action != tt.action {
				t.Fatalf("action = %q, want %q", cmdErr.Action, tt.action)
			}
			if cmdErr.Code != tt.code {
				t.Fatalf("code = %q, want %q", cmdErr.Code, tt.code)
			}
			for key, want := range map[string]any{
				"alias": "target", "hostname": "192.0.2.10", "port": "2222",
				"proxy_jump": "jump", "remote_fingerprint": "SHA256:remote",
			} {
				if got := cmdErr.Details[key]; got != want {
					t.Errorf("details[%q] = %v, want %v", key, got, want)
				}
			}
			if tt.knownFP != "" && cmdErr.Details["known_fingerprint"] != tt.knownFP {
				t.Errorf("known fingerprint = %v", cmdErr.Details["known_fingerprint"])
			}
		})
	}
}

func TestConnErrorToOutputConnectionCodes(t *testing.T) {
	for kind, want := range map[sshclient.ConnErrorKind]string{
		sshclient.ErrNetwork: "network_error",
		sshclient.ErrAuth:    "auth_failed",
	} {
		cmdErr := connErrorToOutput(&sshclient.ConnError{Kind: kind, Host: "192.0.2.10", Port: "22"}, "target")
		if cmdErr.Code != want {
			t.Errorf("kind %v code = %q, want %q", kind, cmdErr.Code, want)
		}
	}
}

func TestConnErrorToOutputFallbackCodes(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   string
		wantAction string
		wantHint   string
	}{
		{
			name:       "deadline takes precedence over net error",
			err:        context.DeadlineExceeded,
			wantCode:   output.CodeTimeout,
			wantAction: "increase --timeout or run the command in background",
			wantHint:   "remote command may still be running",
		},
		{
			name:       "wrapped deadline",
			err:        fmt.Errorf("execution cancelled: %w", context.DeadlineExceeded),
			wantCode:   output.CodeTimeout,
			wantAction: "increase --timeout or run the command in background",
			wantHint:   "remote command may still be running",
		},
		{
			name:       "network error",
			err:        &net.OpError{Op: "dial", Net: "tcp", Err: testNetError{}},
			wantCode:   output.CodeNetworkError,
			wantAction: "check network connectivity",
		},
		{
			name:       "generic error",
			err:        errors.New("unexpected"),
			wantCode:   output.CodeInternalError,
			wantAction: "check connectivity and credentials",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdErr := connErrorToOutput(tt.err, "target")
			if cmdErr.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", cmdErr.Code, tt.wantCode)
			}
			if cmdErr.Action != tt.wantAction {
				t.Fatalf("action = %q, want %q", cmdErr.Action, tt.wantAction)
			}
			if tt.wantHint != "" && !strings.Contains(cmdErr.Hint, tt.wantHint) {
				t.Fatalf("hint = %q, want substring %q", cmdErr.Hint, tt.wantHint)
			}
		})
	}
}

// TestRunErrorToOutput verifies run-phase classification: after a connection
// is established, the only two outcomes are timeout (local deadline) and
// result_indeterminate — internal_error must never appear here because the
// command may already have reached the remote host.
func TestRunErrorToOutput(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   string
		wantAction string
	}{
		{
			name:       "wrapped deadline",
			err:        fmt.Errorf("execution cancelled: %w", context.DeadlineExceeded),
			wantCode:   output.CodeTimeout,
			wantAction: "increase --timeout or run the command in background",
		},
		{
			name:       "connection lost mid-run",
			err:        errors.New("session channel closed"),
			wantCode:   output.CodeResultIndeterminate,
			wantAction: "verify remote state before retrying",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdErr := runErrorToOutput(tt.err)
			if cmdErr.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", cmdErr.Code, tt.wantCode)
			}
			if cmdErr.Action != tt.wantAction {
				t.Fatalf("action = %q, want %q", cmdErr.Action, tt.wantAction)
			}
		})
	}
}

func TestConnErrorToOutputUsesFailingProxyAlias(t *testing.T) {
	cmdErr := connErrorToOutput(&sshclient.ConnError{
		Kind: sshclient.ErrHostKeyUnknown, Alias: "jump", Host: "192.0.2.5", Port: "22",
	}, "target")
	if cmdErr.Action != "run: sshq trust jump" {
		t.Fatalf("action = %q", cmdErr.Action)
	}
}

// TestHostToConnConfigMissingCredentialIgnored verifies the ErrNotFound case:
// a store with no entry for the alias yields an empty password and no error.
func TestHostToConnConfigMissingCredentialIgnored(t *testing.T) {
	// Non-existent file => store reads as empty => Get returns ErrNotFound.
	path := filepath.Join(t.TempDir(), "missing.age")
	creds, err := credential.Open(credential.WithPath(path))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	host := config.Host{Alias: "prod", HostName: "192.0.2.10", Port: "22", User: "tester"}
	cfg, err := hostToConnConfigWithCredentials(host, nil, creds)
	if err != nil {
		t.Fatalf("missing credential should not error, got: %v", err)
	}
	if cfg.Password != "" {
		t.Fatalf("password = %q, want empty for missing credential", cfg.Password)
	}
	if cfg.Host != "192.0.2.10" || cfg.User != "tester" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}
