package remote

import (
	"fmt"
	"strings"
	"time"
)

type OS string

const (
	Linux   OS = "linux"
	Darwin  OS = "darwin"
	FreeBSD OS = "freebsd"
	Windows OS = "windows"
	Unknown OS = "unknown"
)

type Shell string

const (
	Bash       Shell = "bash"
	Ash        Shell = "ash"
	Zsh        Shell = "zsh"
	Sh         Shell = "sh"
	Fish       Shell = "fish"
	Ksh        Shell = "ksh"
	Tcsh       Shell = "tcsh"
	Csh        Shell = "csh"
	PowerShell Shell = "powershell"
	Cmd        Shell = "cmd"
)

type Profile struct {
	OS             OS     `json:"os"`
	Shell          Shell  `json:"shell"`
	Encoding       string `json:"encoding,omitempty"`
	HomeDir        string `json:"home_dir,omitempty"`
	TempDir        string `json:"temp_dir,omitempty"`
	PowerShellPath string `json:"powershell_path,omitempty"`
	PwshPath       string `json:"pwsh_path,omitempty"`
	DetectedAt     int64  `json:"detected_at"`
}

func (p *Profile) IsPOSIX() bool {
	return p.OS != Windows
}

func (p *Profile) IsWindows() bool {
	return p.OS == Windows
}

func (p *Profile) NeedsStdinInjection() bool {
	switch p.Shell {
	case PowerShell, Cmd:
		return true
	default:
		return false
	}
}

func (p *Profile) InterpreterCmd() string {
	switch p.Shell {
	case Bash:
		return "bash -s"
	case Ash:
		return "ash -s"
	case Zsh:
		return "zsh -s"
	case Fish:
		return "fish"
	case Ksh:
		return "ksh -s"
	case Tcsh:
		return "tcsh"
	case Csh:
		return "csh"
	case Sh:
		return "sh -s"
	case PowerShell:
		return "powershell -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand"
	default:
		return "sh -s"
	}
}

func (p *Profile) Age() time.Duration {
	return time.Since(time.Unix(p.DetectedAt, 0))
}

func RenderProfileCompact(p *Profile) string {
	if p == nil {
		return ""
	}
	parts := []string{fmt.Sprintf("os=%s", p.OS), fmt.Sprintf("shell=%s", p.Shell)}
	if p.Encoding != "" {
		parts = append(parts, "encoding="+p.Encoding)
	}
	if p.PowerShellPath != "" {
		parts = append(parts, "powershell_path="+p.PowerShellPath)
	}
	if p.PwshPath != "" {
		parts = append(parts, "pwsh_path="+p.PwshPath)
	}
	return strings.Join(parts, " ")
}

func RenderProfilePretty(p *Profile) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "OS:           %s\n", p.OS)
	fmt.Fprintf(&b, "Shell:        %s\n", p.Shell)
	if p.Encoding != "" {
		fmt.Fprintf(&b, "Encoding:     %s\n", p.Encoding)
	}
	if p.HomeDir != "" {
		fmt.Fprintf(&b, "RemoteHome:   %s\n", p.HomeDir)
	}
	if p.TempDir != "" {
		fmt.Fprintf(&b, "RemoteTemp:   %s\n", p.TempDir)
	}
	if p.PowerShellPath != "" {
		fmt.Fprintf(&b, "PowerShell:   %s\n", p.PowerShellPath)
	}
	if p.PwshPath != "" {
		fmt.Fprintf(&b, "Pwsh:         %s\n", p.PwshPath)
	}
	return b.String()
}
