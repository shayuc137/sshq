package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	osexec "os/exec"
	"strings"

	sshqexec "github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/updater"
	"github.com/shayuc137/sshq/internal/version"
	"github.com/spf13/cobra"
)

type updateRunner interface {
	Run(context.Context, updater.Mode) (updater.Result, error)
}

type updateCommandDeps struct {
	newRunner     func(string) updateRunner
	refreshSkills func(context.Context, string) (skillUpdateResult, string, error)
}

func newUpdateCommand() *cobra.Command {
	return newUpdateCommandWithDeps(updateCommandDeps{
		newRunner:     func(current string) updateRunner { return updater.New(current) },
		refreshSkills: refreshSkillsWithNewBinary,
	})
}

func newUpdateCommandWithDeps(deps updateCommandDeps) *cobra.Command {
	var checkOnly bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update sshq and all installed AI skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			timeout, _ := cmd.Flags().GetDuration("timeout")
			ctx := cmd.Context()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}

			mode := updater.ModeApply
			if checkOnly {
				mode = updater.ModeCheck
			}
			binaryResult, err := deps.newRunner(version.Number()).Run(ctx, mode)
			if err != nil {
				return updateCommandError(err)
			}
			result := updateResult{Result: binaryResult, SkillUpdates: []skillUpdateStatus{}}
			w := writerFrom(cmd.Context())
			if checkOnly || !binaryResult.UpdateAvailable {
				w.Render(result)
				return nil
			}

			skills, warnings, skillErr := deps.refreshSkills(ctx, binaryResult.TargetPath)
			result.SkillUpdates = skills.Updates
			result.SkillError = ""
			if warnings != "" {
				w.Info(warnings)
			}
			if skillErr != nil {
				result.SkillError = skillErr.Error()
				w.Render(result)
				return &sshqexec.ExitError{Code: 2}
			}
			w.Render(result)
			return nil
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "check for an update without installing it")
	return cmd
}

type updateResult struct {
	updater.Result
	SkillUpdates []skillUpdateStatus `json:"skill_updates"`
	SkillError   string              `json:"skill_error,omitempty"`
}

func (r updateResult) Pretty() string {
	if !r.UpdateAvailable {
		return fmt.Sprintf("sshq is up to date: %s", r.CurrentVersion)
	}
	if !r.BinaryUpdated {
		return fmt.Sprintf("update available: %s -> %s", r.CurrentVersion, r.LatestVersion)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "updated sshq: %s -> %s\n", r.CurrentVersion, r.LatestVersion)
	fmt.Fprintf(&b, "asset: %s (checksum verified)\n", r.AssetName)
	updated := 0
	failed := 0
	for _, skill := range r.SkillUpdates {
		if skill.Error != "" {
			failed++
		} else if skill.Updated {
			updated++
		}
	}
	fmt.Fprintf(&b, "skills: %d updated", updated)
	if failed > 0 {
		fmt.Fprintf(&b, ", %d failed", failed)
	}
	if r.SkillError != "" {
		fmt.Fprintf(&b, "\nskill refresh failed: %s", r.SkillError)
	}
	return b.String()
}

func updateCommandError(err error) error {
	var permissionErr *updater.PermissionError
	if errors.As(err, &permissionErr) {
		return output.Errorf(permissionErr.Error(), permissionErr.Action).
			WithCode(output.CodeInternalError).
			WithDetails(map[string]any{
				"target_path": permissionErr.TargetPath,
				"staged_path": permissionErr.StagedPath,
			})
	}
	var rollbackErr *updater.RollbackError
	if errors.As(err, &rollbackErr) {
		return output.Errorf(rollbackErr.Error(), "").
			WithCode(output.CodeInternalError).
			WithDetails(map[string]any{
				"target_path": rollbackErr.TargetPath,
				"old_path":    rollbackErr.OldPath,
				"new_path":    rollbackErr.NewPath,
			})
	}
	return output.Errorf(err.Error(), "").WithCode(output.CodeInternalError)
}

func refreshSkillsWithNewBinary(ctx context.Context, targetPath string) (skillUpdateResult, string, error) {
	var stdout, stderr bytes.Buffer
	cmd := osexec.CommandContext(ctx, targetPath, "--json", "skill", "update")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	var envelope struct {
		Data  skillUpdateResult `json:"data"`
		Error *struct {
			Hint string `json:"hint"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		return skillUpdateResult{}, strings.TrimSpace(stderr.String()), fmt.Errorf("decode new binary skill result: %w", err)
	}
	if envelope.Error != nil {
		if envelope.Error.Hint == "" {
			envelope.Error.Hint = "new binary reported an unknown skill update error"
		}
		return envelope.Data, strings.TrimSpace(stderr.String()), errors.New(envelope.Error.Hint)
	}
	if runErr != nil {
		var exitErr *osexec.ExitError
		if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1 {
			return envelope.Data, strings.TrimSpace(stderr.String()), errors.New("one or more skill installations could not be updated")
		}
		return envelope.Data, strings.TrimSpace(stderr.String()), fmt.Errorf("run new binary skill update: %w", runErr)
	}
	return envelope.Data, strings.TrimSpace(stderr.String()), nil
}
