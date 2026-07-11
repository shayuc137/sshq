package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/credential"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/shayuc137/sshq/internal/sshclient"
)

type fakeDoctorChecker struct {
	outcomes []doctorCheckOutcome
	calls    []doctorCheckName
	host     config.Host
	profile  *remote.Profile
}

func (f *fakeDoctorChecker) Check(_ context.Context, name doctorCheckName, state *doctorRunState) doctorCheckOutcome {
	f.calls = append(f.calls, name)
	if name == doctorConfigValid && len(f.outcomes) > 0 && f.outcomes[0].Value != doctorCheckFailed {
		state.host = f.host
	}
	if name == doctorShellDetected && f.profile != nil {
		state.profile = f.profile
	}
	index := len(f.calls) - 1
	if index >= len(f.outcomes) {
		return passedDoctorOutcome()
	}
	return f.outcomes[index]
}

func healthyDoctorOutcomes(identity doctorCheckValue) []doctorCheckOutcome {
	return []doctorCheckOutcome{
		passedDoctorOutcome(),
		{Value: identity},
		passedDoctorOutcome(),
		passedDoctorOutcome(),
		passedDoctorOutcome(),
		passedDoctorOutcome(),
		passedDoctorOutcome(),
	}
}

func testDoctorHost() config.Host {
	return config.Host{
		Alias:        "target",
		HostName:     "192.0.2.10",
		Port:         "22",
		User:         "tester",
		ProxyJump:    "jump",
		IdentityFile: "~/.ssh/id_test",
	}
}

func TestRunDoctorHealthyPreservesIdentityNullAndProfile(t *testing.T) {
	profile := &remote.Profile{OS: remote.Linux, Shell: remote.Bash, Encoding: "utf-8"}
	checker := &fakeDoctorChecker{
		outcomes: healthyDoctorOutcomes(doctorCheckNotApplicable),
		host:     testDoctorHost(),
		profile:  profile,
	}

	result := runDoctor(context.Background(), "target", checker)
	if result.FailedCheck != "" || result.NextAction != "" {
		t.Fatalf("healthy result = %+v", result)
	}
	if result.Profile != profile {
		t.Fatalf("profile = %+v, want %+v", result.Profile, profile)
	}
	wantCalls := []doctorCheckName{
		doctorConfigValid, doctorIdentityFile, doctorProxyJump, doctorTCPReachable,
		doctorHostKeyKnown, doctorAuthOK, doctorShellDetected,
	}
	if !reflect.DeepEqual(checker.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", checker.calls, wantCalls)
	}

	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"identity_file_exists":null`)) {
		t.Fatalf("identity null missing from JSON: %s", b)
	}
	if bytes.Contains(b, []byte(`"exit_code"`)) {
		t.Fatalf("doctor data must not carry exit_code: %s", b)
	}
}

func TestRunDoctorFailureMappingAndShortCircuit(t *testing.T) {
	tests := []struct {
		name          string
		outcomes      []doctorCheckOutcome
		wantFailed    doctorCheckName
		wantAction    string
		wantCallCount int
	}{
		{
			name:          "missing config",
			outcomes:      []doctorCheckOutcome{failedDoctorOutcome(doctorFailureConfigMissing, "missing", "", "")},
			wantFailed:    doctorConfigValid,
			wantAction:    "sshq config add target --hostname <ip>",
			wantCallCount: 1,
		},
		{
			name:          "invalid config",
			outcomes:      []doctorCheckOutcome{failedDoctorOutcome(doctorFailureConfigInvalid, "bad quoting", "user", "")},
			wantFailed:    doctorConfigValid,
			wantAction:    "sshq config set target user <value>",
			wantCallCount: 1,
		},
		{
			name: "missing identity continues",
			outcomes: []doctorCheckOutcome{
				passedDoctorOutcome(),
				failedDoctorOutcome(doctorFailureIdentityMissing, "missing identity", "", ""),
				passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(),
			},
			wantFailed:    doctorIdentityFile,
			wantAction:    "sshq config set target identityfile <existing-path>",
			wantCallCount: 7,
		},
		{
			name: "missing proxy",
			outcomes: []doctorCheckOutcome{
				passedDoctorOutcome(), passedDoctorOutcome(),
				failedDoctorOutcome(doctorFailureProxyMissing, "missing proxy", "", "jump"),
			},
			wantFailed:    doctorProxyJump,
			wantAction:    "sshq config add jump --hostname <ip>",
			wantCallCount: 3,
		},
		{
			name: "proxy resolution error without safe action",
			outcomes: []doctorCheckOutcome{
				passedDoctorOutcome(), passedDoctorOutcome(),
				failedDoctorOutcome(doctorFailureProxyMissing, "credential store failed", "", ""),
			},
			wantFailed:    doctorProxyJump,
			wantCallCount: 3,
		},
		{
			name: "tcp unreachable",
			outcomes: []doctorCheckOutcome{
				passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(),
				failedDoctorOutcome(doctorFailureTCP, "direct network failed", "", ""),
			},
			wantFailed:    doctorTCPReachable,
			wantCallCount: 4,
		},
		{
			name: "unknown host key",
			outcomes: []doctorCheckOutcome{
				passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(),
				failedDoctorOutcome(doctorFailureHostKeyUnknown, "unknown key", "", ""),
			},
			wantFailed:    doctorHostKeyKnown,
			wantAction:    "sshq trust target",
			wantCallCount: 5,
		},
		{
			name: "mismatched host key",
			outcomes: []doctorCheckOutcome{
				passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(),
				failedDoctorOutcome(doctorFailureHostKeyMismatch, "changed key", "", ""),
			},
			wantFailed:    doctorHostKeyKnown,
			wantAction:    "sshq trust target --replace",
			wantCallCount: 5,
		},
		{
			name: "authentication failed",
			outcomes: []doctorCheckOutcome{
				passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(),
				failedDoctorOutcome(doctorFailureAuth, "auth methods exhausted", "", ""),
			},
			wantFailed:    doctorAuthOK,
			wantCallCount: 6,
		},
		{
			name: "shell detection failed",
			outcomes: []doctorCheckOutcome{
				passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(), passedDoctorOutcome(),
				failedDoctorOutcome(doctorFailureShell, "use explicit shell", "", ""),
			},
			wantFailed:    doctorShellDetected,
			wantCallCount: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &fakeDoctorChecker{outcomes: tt.outcomes, host: testDoctorHost()}
			result := runDoctor(context.Background(), "target", checker)
			if result.FailedCheck != string(tt.wantFailed) {
				t.Fatalf("failed_check = %q, want %q", result.FailedCheck, tt.wantFailed)
			}
			if result.NextAction != tt.wantAction {
				t.Fatalf("next_action = %q, want %q", result.NextAction, tt.wantAction)
			}
			if len(checker.calls) != tt.wantCallCount {
				t.Fatalf("calls = %v, want count %d", checker.calls, tt.wantCallCount)
			}
			for _, check := range result.Checks.ordered()[tt.wantCallCount:] {
				if check.value != doctorCheckSkipped {
					t.Fatalf("check %s = %v, want skipped", check.name, check.value)
				}
			}
		})
	}
}

func TestDoctorPrettyUsesOrderedStatusSymbols(t *testing.T) {
	result := doctorResult{
		Alias:    "target",
		Resolved: doctorResolved{Hostname: "192.0.2.10", Port: "22", User: "tester"},
		Checks: doctorChecks{
			ConfigValid:   doctorCheckPassed,
			IdentityFile:  doctorCheckNotApplicable,
			ProxyJump:     doctorCheckFailed,
			TCPReachable:  doctorCheckSkipped,
			HostKeyKnown:  doctorCheckSkipped,
			AuthOK:        doctorCheckSkipped,
			ShellDetected: doctorCheckSkipped,
		},
		NextAction: "sshq config add jump --hostname <ip>",
	}
	pretty := result.Pretty()
	for _, line := range []string{
		"✓ config_valid", "– identity_file_exists", "✗ proxy_jump_exists",
		"– tcp_reachable", "Next action: sshq config add jump --hostname <ip>",
	} {
		if !strings.Contains(pretty, line) {
			t.Errorf("Pretty() missing %q:\n%s", line, pretty)
		}
	}
}

func TestDoctorPrettySurfacesWindowsExecutablePaths(t *testing.T) {
	result := doctorResult{
		Alias:    "win",
		Resolved: doctorResolved{Hostname: "192.0.2.20", User: "administrator", Port: "22"},
		Checks:   newDoctorChecks(),
		Profile: &remote.Profile{
			OS:             remote.Windows,
			Shell:          remote.PowerShell,
			PowerShellPath: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
			PwshPath:       `C:\Program Files\PowerShell\7\pwsh.exe`,
		},
	}
	pretty := result.Pretty()
	for _, want := range []string{"powershell_path=C:", "pwsh_path=C:"} {
		if !strings.Contains(pretty, want) {
			t.Fatalf("doctor output missing %q: %s", want, pretty)
		}
	}
}

func TestDoctorCommandRendersSuccessEnvelopeBeforeExitOne(t *testing.T) {
	dir := t.TempDir()
	store := configStoreForDoctorTest(t, dir)

	checker := &fakeDoctorChecker{
		outcomes: []doctorCheckOutcome{failedDoctorOutcome(doctorFailureConfigMissing, "missing", "", "")},
	}
	cmd := newDoctorCommandWithChecker(func(*config.Store, *credential.Store, *remote.Cache, time.Duration) doctorChecker {
		return checker
	})
	out := &bytes.Buffer{}
	ctx := withConfig(context.Background(), store)
	ctx = withWriter(ctx, output.New(out, &bytes.Buffer{}, output.WithJSON()))
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"missing-alias"})

	err := cmd.Execute()
	var badNews *output.BadNewsError
	if !errors.As(err, &badNews) || badNews.ProcessExitCode() != 1 {
		t.Fatalf("Execute() error = %v, want exit 1", err)
	}
	var envelope struct {
		ExitCode *int `json:"exit_code"`
		Data     struct {
			FailedCheck string `json:"failed_check"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if envelope.ExitCode != nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	if envelope.Data.FailedCheck != string(doctorConfigValid) {
		t.Fatalf("data = %+v", envelope.Data)
	}
}

func TestDoctorShellDetectedBypassesAndRepairsStaleCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	cache, err := remote.NewCache(time.Hour, remote.WithCachePath(path))
	if err != nil {
		t.Fatal(err)
	}
	stale := &remote.Profile{OS: remote.Windows, Shell: remote.Cmd, DetectedAt: time.Now().Unix()}
	fresh := &remote.Profile{OS: remote.Linux, Shell: remote.Bash, DetectedAt: time.Now().Unix()}
	cache.Put("192.0.2.10", "22", stale)

	checker := &systemDoctorChecker{
		cache: cache,
		detect: func(context.Context, *sshclient.Client) (*remote.Profile, error) {
			return fresh, nil
		},
	}
	state := &doctorRunState{
		connCfg: sshclient.ConnConfig{Host: "192.0.2.10", Port: "22"},
	}

	outcome := checker.shellDetected(context.Background(), state)
	if outcome.Value != doctorCheckPassed || state.profile != fresh {
		t.Fatalf("outcome = %+v, profile = %+v", outcome, state.profile)
	}
	got, ok := cache.Get("192.0.2.10", "22")
	if !ok || got.Shell != remote.Bash {
		t.Fatalf("cached profile = %+v, ok=%v; want fresh bash profile", got, ok)
	}
}

func configStoreForDoctorTest(t *testing.T, dir string) *config.Store {
	t.Helper()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("Host target\n  HostName 192.0.2.10\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
