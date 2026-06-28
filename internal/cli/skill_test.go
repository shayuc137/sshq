package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if !strings.Contains(out.String(), "exported 5 files to "+dir) {
		t.Fatalf("export output = %q", out.String())
	}

	for _, rel := range []string{
		"SKILL.md",
		filepath.Join("references", "exec-transfer.md"),
		filepath.Join("references", "config.md"),
		filepath.Join("references", "cluster-tunnel.md"),
		filepath.Join("references", "discovery.md"),
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
	cmd.SetArgs([]string{"--pretty", "skill", "install", "--scope", "project", "--target", "codex"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill install failed: %v", err)
	}

	skillPath := filepath.Join(project, ".codex", "skills", "sshq", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("stale"), 0644); err != nil {
		t.Fatalf("seed stale skill file: %v", err)
	}

	cmd, _, _ = rootCommandForTest(t)
	cmd.SetArgs([]string{"--pretty", "skill", "install", "--scope", "project", "--target", "codex"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("repeat skill install failed: %v", err)
	}

	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if !strings.Contains(string(data), `sshq_version: "0.1.0"`) {
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
	cmd.SetArgs([]string{"--pretty", "skill", "install", "--scope", "project", "--target", "codex"})
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
	if env.Data.CurrentVersion != "0.1.0" {
		t.Fatalf("current version = %q", env.Data.CurrentVersion)
	}
	if len(env.Data.Installations) != 2 {
		t.Fatalf("installations = %+v", env.Data.Installations)
	}

	found := map[string]bool{}
	for _, inst := range env.Data.Installations {
		if inst.SSHQVersion != "0.1.0" || !inst.MatchesCurrent {
			t.Fatalf("installation mismatch: %+v", inst)
		}
		found[inst.Target+"-"+inst.Scope] = true
	}
	if !found["claude-user"] || !found["codex-project"] {
		t.Fatalf("missing expected installations: %+v", found)
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
