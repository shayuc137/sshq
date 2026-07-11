package remote

import "strings"

// SuspectStaleProfile recognizes only shell-mismatch signals that are strong
// enough to justify discarding a cached profile.
func SuspectStaleProfile(p *Profile, stdout, stderr string) bool {
	if p == nil {
		return false
	}

	stdout = strings.ToLower(stdout)
	stderr = strings.ToLower(stderr)
	switch p.Shell {
	case PowerShell:
		return strings.Contains(stderr, "'powershell' is not recognized") ||
			strings.Contains(stderr, "powershell: not found")
	case Cmd:
		return strings.HasPrefix(stdout, "#< clixml") || strings.HasPrefix(stderr, "#< clixml")
	case Bash, Ash, Zsh, Sh:
		return strings.Contains(stderr, "is not recognized as an internal or external command")
	default:
		return false
	}
}
