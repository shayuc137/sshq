package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/version"
	sshqskill "github.com/shayuc137/sshq/skills/sshq"
	"github.com/spf13/cobra"
)

const (
	skillScopeUser    = "user"
	skillScopeProject = "project"

	skillTargetClaude = "claude"
	skillTargetCodex  = "codex"
)

func newSkillCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install and inspect sshq AI skill files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newSkillInstallCommand(),
		newSkillExportCommand(),
		newSkillStatusCommand(),
	)
	return cmd
}

func newSkillInstallCommand() *cobra.Command {
	var opts skillInstallOptions
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the embedded sshq skill",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := skillTargetClaude
			if opts.codex {
				target = skillTargetCodex
			}
			scope := skillScopeUser
			if opts.project {
				scope = skillScopeProject
			}
			dir, err := resolveSkillInstallDir(scope, target)
			if err != nil {
				return err
			}

			files, err := writeEmbeddedSkill(dir, opts.dryRun)
			if err != nil {
				return err
			}

			w := writerFrom(cmd.Context())
			if opts.dryRun {
				w.Render(skillDryRunResult{Files: files})
				return nil
			}
			w.Render(skillWriteSummary{Action: "installed", FileCount: len(files), Directory: dir})
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.project, "project", false, "install at project level instead of user level")
	cmd.Flags().BoolVar(&opts.codex, "codex", false, "install for Codex instead of Claude Code")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "print file paths without writing")
	return cmd
}

func newSkillExportCommand() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export the embedded sshq skill files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			files, err := writeEmbeddedSkill(dir, false)
			if err != nil {
				return err
			}
			w := writerFrom(cmd.Context())
			w.Render(skillWriteSummary{Action: "exported", FileCount: len(files), Directory: dir})
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "./sshq-skill/", "output directory")
	return cmd
}

func newSkillStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show installed sshq skill versions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := inspectSkillInstallations()
			if err != nil {
				return err
			}
			w := writerFrom(cmd.Context())
			w.Render(status)
			return nil
		},
	}
}

type skillInstallOptions struct {
	project bool
	codex   bool
	dryRun  bool
}

type skillWriteSummary struct {
	Action    string `json:"action"`
	FileCount int    `json:"file_count"`
	Directory string `json:"directory"`
}

func (s skillWriteSummary) Pretty() string {
	return fmt.Sprintf("%s %d files to %s", s.Action, s.FileCount, s.Directory)
}

type skillDryRunResult struct {
	Files []string `json:"files"`
}

func (r skillDryRunResult) Pretty() string {
	return strings.Join(r.Files, "\n")
}

type skillStatusResult struct {
	CurrentVersion string                    `json:"current_version"`
	Installations  []skillInstallationStatus `json:"installations"`
}

func (r skillStatusResult) Pretty() string {
	if len(r.Installations) == 0 {
		return "no sshq skill installations found"
	}

	var b strings.Builder
	for _, s := range r.Installations {
		match := "no"
		if s.MatchesCurrent {
			match = "yes"
		}
		installedVersion := s.SSHQVersion
		if installedVersion == "" {
			installedVersion = "unknown"
		}
		fmt.Fprintf(&b, "%s %s | %s | version=%s | current=%s | match=%s",
			s.Target, s.Scope, s.Path, installedVersion, s.CurrentVersion, match)
		if s.Error != "" {
			fmt.Fprintf(&b, " | error=%s", s.Error)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

type skillInstallationStatus struct {
	Target         string `json:"target"`
	Scope          string `json:"scope"`
	Path           string `json:"path"`
	SSHQVersion    string `json:"sshq_version,omitempty"`
	CurrentVersion string `json:"current_version"`
	MatchesCurrent bool   `json:"matches_current"`
	Error          string `json:"error,omitempty"`
}

type skillInstallLocation struct {
	target string
	scope  string
	dir    string
}

func writeEmbeddedSkill(dir string, dryRun bool) ([]string, error) {
	files, err := embeddedSkillFiles()
	if err != nil {
		return nil, err
	}

	destinations := make([]string, 0, len(files))
	for _, file := range files {
		dest := filepath.Join(dir, filepath.FromSlash(file))
		destinations = append(destinations, dest)
		if dryRun {
			continue
		}

		data, err := sshqskill.FS.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read embedded skill file %s: %w", file, err)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return nil, fmt.Errorf("create skill directory %s: %w", filepath.Dir(dest), err)
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			return nil, fmt.Errorf("write skill file %s: %w", dest, err)
		}
	}
	return destinations, nil
}

func embeddedSkillFiles() ([]string, error) {
	files := []string{}
	err := fs.WalkDir(sshqskill.FS, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		files = append(files, name)
		return nil
	})
	sort.Strings(files)
	return files, err
}

func resolveSkillInstallDir(scope, target string) (string, error) {
	if scope != skillScopeUser && scope != skillScopeProject {
		return "", output.Errorf("invalid skill scope: "+scope, "use --scope user or --scope project")
	}
	targetDir, err := skillTargetDir(target)
	if err != nil {
		return "", err
	}
	if scope == skillScopeProject {
		return filepath.Join(targetDir, "skills", "sshq"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", output.Errorf("home directory not found", "set HOME or use --scope project")
	}
	return filepath.Join(home, targetDir, "skills", "sshq"), nil
}

func skillTargetDir(target string) (string, error) {
	switch target {
	case skillTargetClaude:
		return ".claude", nil
	case skillTargetCodex:
		return ".codex", nil
	default:
		return "", output.Errorf("invalid skill target: "+target, "use --codex for Codex, omit for Claude Code")
	}
}

func inspectSkillInstallations() (skillStatusResult, error) {
	locations, err := knownSkillInstallLocations()
	if err != nil {
		return skillStatusResult{}, err
	}

	current := currentSkillVersion()
	result := skillStatusResult{
		CurrentVersion: current,
		Installations:  []skillInstallationStatus{},
	}

	for _, loc := range locations {
		info, err := os.Stat(loc.dir)
		if os.IsNotExist(err) {
			continue
		}

		status := skillInstallationStatus{
			Target:         loc.target,
			Scope:          loc.scope,
			Path:           loc.dir,
			CurrentVersion: current,
		}
		if err != nil {
			status.Error = err.Error()
			result.Installations = append(result.Installations, status)
			continue
		}
		if !info.IsDir() {
			status.Error = "path is not a directory"
			result.Installations = append(result.Installations, status)
			continue
		}

		installedVersion, err := readInstalledSkillVersion(loc.dir)
		if err != nil {
			status.Error = err.Error()
		} else {
			status.SSHQVersion = installedVersion
			status.MatchesCurrent = installedVersion == current
		}
		result.Installations = append(result.Installations, status)
	}

	return result, nil
}

func knownSkillInstallLocations() ([]skillInstallLocation, error) {
	pairs := []struct {
		target string
		scope  string
	}{
		{skillTargetClaude, skillScopeUser},
		{skillTargetClaude, skillScopeProject},
		{skillTargetCodex, skillScopeUser},
		{skillTargetCodex, skillScopeProject},
	}

	locations := make([]skillInstallLocation, 0, len(pairs))
	for _, pair := range pairs {
		dir, err := resolveSkillInstallDir(pair.scope, pair.target)
		if err != nil {
			return nil, err
		}
		locations = append(locations, skillInstallLocation{
			target: pair.target,
			scope:  pair.scope,
			dir:    dir,
		})
	}
	return locations, nil
}

func readInstalledSkillVersion(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return "", err
	}
	skillVersion, ok := parseSkillVersion(string(data))
	if !ok {
		return "", fmt.Errorf("sshq_version not found in SKILL.md")
	}
	return skillVersion, nil
}

func parseSkillVersion(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", false
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "sshq_version" {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		return value, value != ""
	}
	return "", false
}

func currentSkillVersion() string {
	return strings.TrimPrefix(version.Version, "v")
}
