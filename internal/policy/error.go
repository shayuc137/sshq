package policy

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/shayuc137/sshq/internal/output"
)

type BlockedError struct {
	Alias   string
	Kind    string
	Reason  string
	Pattern string
	Input   string
}

func (e *BlockedError) Error() string {
	return e.ToOutputError().Error()
}

func (e *BlockedError) ToOutputError() *output.CmdError {
	if e == nil {
		return output.Errorf("policy blocked request", "")
	}
	return output.Errorf(e.hint(), e.action())
}

func (e *BlockedError) hint() string {
	alias := e.Alias
	if alias == "" {
		alias = "unknown"
	}

	switch e.Reason {
	case ReasonWhitelistMiss:
		return fmt.Sprintf("command blocked on host %q: no whitelist rule matched", alias)
	case ReasonBlacklistMatch:
		return fmt.Sprintf("command blocked on host %q: matches blacklist '%s'", alias, e.Pattern)
	case ReasonLocalPathDenied:
		return fmt.Sprintf("local path blocked on host %q: %s is outside the whitelist", alias, truncateForError(e.Input))
	case ReasonRemotePathDenied:
		return fmt.Sprintf("remote path blocked on host %q: %s is outside the whitelist", alias, truncateForError(e.Input))
	case ReasonConfigError:
		return fmt.Sprintf("policy configuration invalid for host %q: %s", alias, e.Pattern)
	default:
		return fmt.Sprintf("request blocked by policy on host %q: %s", alias, e.Reason)
	}
}

func (e *BlockedError) action() string {
	switch e.Reason {
	case ReasonWhitelistMiss:
		return fmt.Sprintf("to allow temporarily: sshq policy grant %s %s --ttl 1h", e.Alias, shellQuote("<command-regex>"))
	case ReasonBlacklistMatch:
		return "edit config.toml to remove or narrow the blacklist; temporary grants do not override blacklists"
	case ReasonLocalPathDenied:
		return fmt.Sprintf("to allow temporarily: sshq policy grant %s %s --kind local-path --ttl 1h", e.Alias, shellQuote(e.Input))
	case ReasonRemotePathDenied:
		return fmt.Sprintf("to allow temporarily: sshq policy grant %s %s --kind remote-path --ttl 1h", e.Alias, shellQuote(e.Input))
	case ReasonConfigError:
		return "fix config.toml, then rerun the command"
	default:
		return "inspect policy with: sshq policy list " + e.Alias
	}
}

func truncateForError(s string) string {
	s = redactSensitive(s)
	const limit = 120
	if utf8.RuneCountInString(s) <= limit {
		return fmt.Sprintf("%q", s)
	}
	runes := []rune(s)
	return fmt.Sprintf("%q", string(runes[:limit])+"...")
}

func redactSensitive(s string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(password|passwd|pass|token|secret)=\S+`),
		regexp.MustCompile(`(?i)(password|passwd|pass|token|secret)\s+\S+`),
	}
	for _, re := range patterns {
		s = re.ReplaceAllStringFunc(s, func(match string) string {
			if i := strings.IndexAny(match, "= \t"); i >= 0 {
				return match[:i+1] + "<redacted>"
			}
			return "<redacted>"
		})
	}
	return s
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
