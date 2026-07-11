package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	sshqexec "github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/updater"
	"github.com/spf13/cobra"
)

type fakeUpdateRunner struct {
	result updater.Result
	err    error
}

func (f fakeUpdateRunner) Run(context.Context, updater.Mode) (updater.Result, error) {
	return f.result, f.err
}

func TestUpdateCheckAvailableJSONAndExitCode(t *testing.T) {
	result := updater.Result{CurrentVersion: "0.3.0", LatestVersion: "0.4.0", UpdateAvailable: true, AssetName: "sshq_linux_amd64.tar.gz"}
	cmd, out, _ := updateCommandForTest(t, output.WithJSON(), updateCommandDeps{
		newRunner: func(string) updateRunner { return fakeUpdateRunner{result: result} },
		refreshSkills: func(context.Context, string) (skillUpdateResult, string, error) {
			t.Fatal("check mode refreshed skills")
			return skillUpdateResult{}, "", nil
		},
	})
	cmd.SetArgs([]string{"--check"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("error = %v, want process exit 0", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if _, hasError := envelope["error"]; hasError || envelope["exit_code"] != nil {
		t.Fatalf("envelope = %#v", envelope)
	}
	data := envelope["data"].(map[string]any)
	if data["update_available"] != true {
		t.Fatalf("data = %#v", data)
	}
}

func TestUpdateCheckFailureUsesExitTwo(t *testing.T) {
	cmd, _, _ := updateCommandForTest(t, output.WithPretty(), updateCommandDeps{
		newRunner: func(string) updateRunner { return fakeUpdateRunner{err: errors.New("network unavailable")} },
		refreshSkills: func(context.Context, string) (skillUpdateResult, string, error) {
			return skillUpdateResult{}, "", nil
		},
	})
	cmd.SetArgs([]string{"--check"})
	err := cmd.Execute()
	var cmdErr *output.CmdError
	if !errors.As(err, &cmdErr) || cmdErr.ProcessExitCode() != 2 {
		t.Fatalf("error = %v, want command exit 2", err)
	}
}

func TestUpdateApplySkillPartialFailureIsSingleResult(t *testing.T) {
	result := updater.Result{
		CurrentVersion: "0.3.0", LatestVersion: "0.4.0", UpdateAvailable: true,
		AssetName: "sshq_linux_amd64.tar.gz", ChecksumVerified: true, BinaryUpdated: true, TargetPath: "/tmp/sshq",
	}
	cmd, out, _ := updateCommandForTest(t, output.WithJSON(), updateCommandDeps{
		newRunner: func(string) updateRunner { return fakeUpdateRunner{result: result} },
		refreshSkills: func(context.Context, string) (skillUpdateResult, string, error) {
			return skillUpdateResult{Updates: []skillUpdateStatus{{Target: "claude", Error: "permission denied"}}}, "", errors.New("one or more skill installations could not be updated")
		},
	})
	err := cmd.Execute()
	var exitErr *sshqexec.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("error = %v, want exit 2", err)
	}
	if lines := strings.Count(strings.TrimSpace(out.String()), "\n") + 1; lines != 1 {
		t.Fatalf("stdout has %d lines: %q", lines, out.String())
	}
	var envelope struct {
		Data struct {
			BinaryUpdated bool                `json:"binary_updated"`
			SkillUpdates  []skillUpdateStatus `json:"skill_updates"`
			SkillError    string              `json:"skill_error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Data.BinaryUpdated || len(envelope.Data.SkillUpdates) != 1 || envelope.Data.SkillError == "" {
		t.Fatalf("envelope = %+v", envelope)
	}
}

func TestUpdatePrettyOutputIsEnglish(t *testing.T) {
	result := updater.Result{CurrentVersion: "0.4.0", LatestVersion: "0.4.0"}
	cmd, out, _ := updateCommandForTest(t, output.WithPretty(), updateCommandDeps{
		newRunner: func(string) updateRunner { return fakeUpdateRunner{result: result} },
		refreshSkills: func(context.Context, string) (skillUpdateResult, string, error) {
			return skillUpdateResult{}, "", nil
		},
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "sshq is up to date: 0.4.0" {
		t.Fatalf("pretty output = %q", got)
	}
}

func TestRootRegistersUpdateCommand(t *testing.T) {
	cmd, _, err := NewRootCommand().Find([]string{"update"})
	if err != nil || cmd.CommandPath() != "sshq update" {
		t.Fatalf("update command = %v, %v", cmd, err)
	}
}

func updateCommandForTest(t *testing.T, option output.Option, deps updateCommandDeps) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := newUpdateCommandWithDeps(deps)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(withWriter(context.Background(), output.New(&out, &errOut, option)))
	return cmd, &out, &errOut
}
