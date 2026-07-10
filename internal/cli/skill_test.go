package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sshqexec "github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/version"
	"github.com/spf13/cobra"
)

func TestSkillInstallDryRunDoesNotWrite(t *testing.T) {
	home := t.TempDir()
	setHomeForSkillTest(t, home)

	cmd, out, _ := rootCommandForTest(t)
	cmd.SetArgs([]string{"--pretty", "skill", "install", "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill install --dry-run failed: %v", err)
	}

	wantPath := filepath.Join(home, ".claude", "skills", "sshq", "SKILL.md")
	if !strings.Contains(out.String(), wantPath) {
		t.Fatalf("dry-run output = %q, want %q", out.String(), wantPath)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote user skill directory, stat err = %v", err)
	}
}

func TestSkillExportWritesEmbeddedFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sshq-skill")

	cmd, out, _ := rootCommandForTest(t)
	cmd.SetArgs([]string{"--pretty", "skill", "export", "--dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill export failed: %v", err)
	}
	if !strings.Contains(out.String(), "exported 9 files to "+dir) {
		t.Fatalf("export output = %q", out.String())
	}

	for _, rel := range []string{
		"SKILL.md",
		filepath.Join("references", "exec-transfer.md"),
		filepath.Join("references", "config.md"),
		filepath.Join("references", "cluster-tunnel.md"),
		filepath.Join("references", "policy.md"),
		filepath.Join("references", "discovery.md"),
		filepath.Join("references", "windows-paths.md"),
		filepath.Join("references", "windows-background.md"),
		filepath.Join("references", "remote-windows-support.md"),
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("exported file %s missing: %v", rel, err)
		}
	}
}

func TestSkillInstallProjectOverwrites(t *testing.T) {
	project := t.TempDir()
	withWorkingDirForSkillTest(t, project)

	cmd, _, _ := rootCommandForTest(t)
	cmd.SetArgs([]string{"--pretty", "skill", "install", "--project", "--codex"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill install failed: %v", err)
	}

	skillPath := filepath.Join(project, ".codex", "skills", "sshq", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("stale"), 0644); err != nil {
		t.Fatalf("seed stale skill file: %v", err)
	}

	cmd, _, _ = rootCommandForTest(t)
	cmd.SetArgs([]string{"--pretty", "skill", "install", "--project", "--codex"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("repeat skill install failed: %v", err)
	}

	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if !strings.Contains(string(data), fmt.Sprintf("sshq_version: %q", currentSkillVersion())) {
		t.Fatalf("installed skill was not overwritten with embedded content: %q", string(data))
	}
}

func TestSkillStatusReportsInstalledVersions(t *testing.T) {
	home := t.TempDir()
	setHomeForSkillTest(t, home)
	project := t.TempDir()
	withWorkingDirForSkillTest(t, project)

	cmd, _, _ := rootCommandForTest(t)
	cmd.SetArgs([]string{"--pretty", "skill", "install"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install claude user skill: %v", err)
	}

	cmd, _, _ = rootCommandForTest(t)
	cmd.SetArgs([]string{"--pretty", "skill", "install", "--project", "--codex"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install codex project skill: %v", err)
	}

	cmd, out, _ := rootCommandForTest(t)
	cmd.SetArgs([]string{"--json", "skill", "status"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill status failed: %v", err)
	}

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			CurrentVersion string `json:"current_version"`
			Installations  []struct {
				Target         string `json:"target"`
				Scope          string `json:"scope"`
				SSHQVersion    string `json:"sshq_version"`
				MatchesCurrent bool   `json:"matches_current"`
			} `json:"installations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("invalid status JSON: %v\n%s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("status envelope ok=false: %+v", env)
	}
	if env.Data.CurrentVersion != currentSkillVersion() {
		t.Fatalf("current version = %q", env.Data.CurrentVersion)
	}
	if len(env.Data.Installations) != 2 {
		t.Fatalf("installations = %+v", env.Data.Installations)
	}

	found := map[string]bool{}
	for _, inst := range env.Data.Installations {
		if inst.SSHQVersion != currentSkillVersion() || !inst.MatchesCurrent {
			t.Fatalf("installation mismatch: %+v", inst)
		}
		found[inst.Target+"-"+inst.Scope] = true
	}
	if !found["claude-user"] || !found["codex-project"] {
		t.Fatalf("missing expected installations: %+v", found)
	}
}

func TestSkillUpdateRefreshesExistingInstallationsOnly(t *testing.T) {
	home, project, marker := setupSkillFilesystemTest(t)
	claudeUser := filepath.Join(home, ".claude", "skills", "sshq")
	codexProject := filepath.Join(project, ".codex", "skills", "sshq")
	writeInstalledSkillVersion(t, claudeUser, "0.1.0")
	writeInstalledSkillVersion(t, codexProject, "0.1.1")
	if err := os.WriteFile(marker, []byte(currentSkillVersion()+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd, out, _ := rootCommandForTest(t)
	cmd.SetArgs([]string{"--json", "skill", "update"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill update failed: %v", err)
	}

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Updates []skillUpdateStatus `json:"updates"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("invalid update JSON: %v\n%s", err, out.String())
	}
	if !env.OK || len(env.Data.Updates) != 2 {
		t.Fatalf("update result = %+v", env)
	}
	for _, update := range env.Data.Updates {
		if !update.Updated || update.FromVersion == currentSkillVersion() || update.ToVersion != currentSkillVersion() {
			t.Fatalf("unexpected update: %+v", update)
		}
	}
	for _, dir := range []string{claudeUser, codexProject} {
		got, err := readInstalledSkillVersion(dir)
		if err != nil || got != currentSkillVersion() {
			t.Fatalf("installed version at %s = %q, %v", dir, got, err)
		}
	}
	for _, skipped := range []string{
		filepath.Join(home, ".codex", "skills", "sshq"),
		filepath.Join(project, ".claude", "skills", "sshq"),
	} {
		if _, err := os.Stat(skipped); !os.IsNotExist(err) {
			t.Fatalf("update created uninstalled target %s: %v", skipped, err)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("successful update did not clear marker: %v", err)
	}
}

func TestSkillUpdateWithNoInstallations(t *testing.T) {
	setupSkillFilesystemTest(t)

	cmd, out, _ := rootCommandForTest(t)
	cmd.SetArgs([]string{"--pretty", "skill", "update"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill update failed: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "no sshq skill installations found; nothing to update" {
		t.Fatalf("update output = %q", got)
	}
}

func TestSkillUpdatePartialFailureReturnsExitOne(t *testing.T) {
	home, project, marker := setupSkillFilesystemTest(t)
	claudeUser := filepath.Join(home, ".claude", "skills", "sshq")
	writeInstalledSkillVersion(t, claudeUser, "0.1.0")
	codexProject := filepath.Join(project, ".codex", "skills", "sshq")
	if err := os.MkdirAll(filepath.Dir(codexProject), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexProject, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte(currentSkillVersion()+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd, out, _ := rootCommandForTest(t)
	cmd.SetArgs([]string{"--pretty", "skill", "update"})
	err := cmd.Execute()
	var exitErr *sshqexec.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("skill update error = %v, want exit 1", err)
	}
	if !strings.Contains(out.String(), "claude user | 0.1.0 -> "+currentSkillVersion()+" | updated") {
		t.Fatalf("successful update missing from output: %q", out.String())
	}
	if !strings.Contains(out.String(), "codex project | unknown -> "+currentSkillVersion()+" | failed:") {
		t.Fatalf("failed update missing from output: %q", out.String())
	}
	if got, err := readInstalledSkillVersion(claudeUser); err != nil || got != currentSkillVersion() {
		t.Fatalf("successful installation was not refreshed: version=%q err=%v", got, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("partial failure cleared marker: %v", err)
	}
}

func TestSkillOutdatedReminderIsDeduplicatedAndKeepsJSONStdoutClean(t *testing.T) {
	home, _, marker := setupSkillFilesystemTest(t)
	writeInstalledSkillVersion(t, filepath.Join(home, ".claude", "skills", "sshq"), "0.1.0")

	cmd, out, errOut := rootCommandForTest(t)
	cmd.SetArgs([]string{"--json", "ls"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ls failed: %v", err)
	}
	if !strings.Contains(errOut.String(), "skill outdated: claude 0.1.0 (binary "+currentSkillVersion()+") — run 'sshq skill update'") {
		t.Fatalf("reminder stderr = %q", errOut.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("reminder polluted JSON stdout: %v\n%s", err, out.String())
	}
	if got, err := os.ReadFile(marker); err != nil || strings.TrimSpace(string(got)) != currentSkillVersion() {
		t.Fatalf("marker = %q, %v", got, err)
	}

	cmd, _, errOut = rootCommandForTest(t)
	cmd.SetArgs([]string{"--json", "ls"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("second ls failed: %v", err)
	}
	if strings.Contains(errOut.String(), "skill outdated:") {
		t.Fatalf("duplicate reminder stderr = %q", errOut.String())
	}

	originalVersion := version.Version
	version.Version = "99.99.99"
	t.Cleanup(func() { version.Version = originalVersion })
	cmd, _, errOut = rootCommandForTest(t)
	cmd.SetArgs([]string{"--pretty", "ls"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ls after binary upgrade failed: %v", err)
	}
	if !strings.Contains(errOut.String(), "(binary 99.99.99)") {
		t.Fatalf("new binary version did not remind again: %q", errOut.String())
	}
}

func TestSkillInstallClearsReminderMarker(t *testing.T) {
	_, _, marker := setupSkillFilesystemTest(t)
	if err := os.WriteFile(marker, []byte(currentSkillVersion()+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd, _, _ := rootCommandForTest(t)
	cmd.SetArgs([]string{"--pretty", "skill", "install"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill install failed: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("successful install did not clear marker: %v", err)
	}
}

func TestSkillOutdatedReminderZeroInstallationsDoesNotTouchMarker(t *testing.T) {
	setupSkillFilesystemTest(t)
	marker := filepath.Join(t.TempDir(), "missing", "marker")
	t.Setenv(skillReminderMarkerPathEnv, marker)
	root := NewRootCommand()
	cmd, _, err := root.Find([]string{"ls"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	w := output.New(&out, &errOut, output.WithPretty())

	warnIfSkillOutdated(cmd, w)

	if errOut.Len() != 0 {
		t.Fatalf("zero-install reminder stderr = %q", errOut.String())
	}
	if _, err := os.Stat(filepath.Dir(marker)); !os.IsNotExist(err) {
		t.Fatalf("zero-install check touched marker directory: %v", err)
	}
}

func TestSkillOutdatedReminderTreatsDamagedVersionAsUnknown(t *testing.T) {
	home, _, _ := setupSkillFilesystemTest(t)
	dir := filepath.Join(home, ".codex", "skills", "sshq")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("damaged"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd, _, errOut := rootCommandForTest(t)
	cmd.SetArgs([]string{"--pretty", "ls"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ls failed: %v", err)
	}
	if !strings.Contains(errOut.String(), "skill outdated: codex unknown") {
		t.Fatalf("damaged installation reminder = %q", errOut.String())
	}
}

func TestSkillOutdatedReminderMarkerWriteFailureDoesNotBlockCommand(t *testing.T) {
	home, _, _ := setupSkillFilesystemTest(t)
	writeInstalledSkillVersion(t, filepath.Join(home, ".claude", "skills", "sshq"), "0.1.0")
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block marker parent"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(skillReminderMarkerPathEnv, filepath.Join(blocker, "marker"))

	cmd, _, errOut := rootCommandForTest(t)
	cmd.SetArgs([]string{"--pretty", "ls"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("marker write failure blocked command: %v", err)
	}
	if !strings.Contains(errOut.String(), "skill outdated: claude 0.1.0") {
		t.Fatalf("marker write failure suppressed reminder: %q", errOut.String())
	}
}

func TestSkillOutdatedReminderExemptCommands(t *testing.T) {
	tests := []string{
		"sshq version",
		"sshq skill",
		"sshq skill status",
		"sshq daemon",
		"sshq daemon status",
	}
	for _, commandPath := range tests {
		t.Run(commandPath, func(t *testing.T) {
			cmd := &cobra.Command{Use: strings.TrimPrefix(commandPath, "sshq ")}
			root := &cobra.Command{Use: "sshq"}
			if commandPath == "sshq version" || commandPath == "sshq skill" || commandPath == "sshq daemon" {
				root.AddCommand(cmd)
			} else {
				parts := strings.Split(strings.TrimPrefix(commandPath, "sshq "), " ")
				parent := &cobra.Command{Use: parts[0]}
				cmd.Use = parts[1]
				parent.AddCommand(cmd)
				root.AddCommand(parent)
			}
			if !skipSkillOutdatedReminder(cmd) {
				t.Fatalf("%s should skip skill reminder", commandPath)
			}
		})
	}
}

func setupSkillFilesystemTest(t *testing.T) (home, project, marker string) {
	t.Helper()
	home = t.TempDir()
	project = t.TempDir()
	setHomeForSkillTest(t, home)
	withWorkingDirForSkillTest(t, project)
	marker = filepath.Join(t.TempDir(), "skill-update-reminder")
	t.Setenv(skillReminderMarkerPathEnv, marker)
	return home, project, marker
}

func writeInstalledSkillVersion(t *testing.T, dir, skillVersion string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: sshq\nsshq_version: \"" + skillVersion + "\"\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func setHomeForSkillTest(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func withWorkingDirForSkillTest(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
}
