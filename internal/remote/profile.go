package remote

import "time"

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
	OS         OS     `json:"os"`
	Shell      Shell  `json:"shell"`
	Encoding   string `json:"encoding,omitempty"`
	HomeDir    string `json:"home_dir,omitempty"`
	DetectedAt int64  `json:"detected_at"`
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
		return "powershell -NoProfile -NonInteractive -Command -"
	default:
		return "sh -s"
	}
}

func (p *Profile) Age() time.Duration {
	return time.Since(time.Unix(p.DetectedAt, 0))
}
