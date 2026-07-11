package policy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shayuc137/sshq/internal/appconfig"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/policy"
)

func TestWhitelistEmpty(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
command_blacklist = []
`)
	decision := checker.CheckCommand("prod", "anything")
	if !decision.Allowed {
		t.Fatalf("decision = %#v, want allowed", decision)
	}
}

func TestWhitelistBlock(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
command_whitelist = ["^journalctl(\\s|$)"]
`)
	decision := checker.CheckCommand("prod", "rm -rf /tmp/x")
	if decision.Allowed || decision.Reason != policy.ReasonWhitelistMiss {
		t.Fatalf("decision = %#v, want whitelist miss", decision)
	}
}

func TestBlacklistMatch(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
command_blacklist = ["(?i)(^|[;&|])\\s*rm\\b"]
`)
	decision := checker.CheckCommand("prod", "rm -rf /tmp/x")
	if decision.Allowed || decision.Reason != policy.ReasonBlacklistMatch {
		t.Fatalf("decision = %#v, want blacklist match", decision)
	}
	if decision.Pattern == "" {
		t.Fatal("blacklist decision should include pattern")
	}
}

func TestWhitelistBeforeBlacklist(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
command_whitelist = ["^journalctl(\\s|$)"]
command_blacklist = ["(?i)rm\\b"]
`)
	decision := checker.CheckCommand("prod", "rm -rf /tmp/x")
	if decision.Allowed || decision.Reason != policy.ReasonWhitelistMiss {
		t.Fatalf("decision = %#v, want whitelist miss before blacklist", decision)
	}
}

func TestHostOverride(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
command_whitelist = ["^journalctl"]
command_blacklist = ["rm"]

[policy.hosts.prod]
mode = "override"
command_whitelist = ["^uptime$"]
`)
	effective := checker.EffectivePolicy("prod")
	if strings.Join(effective.CommandWhitelist, ",") != "^uptime$" {
		t.Fatalf("whitelist = %#v", effective.CommandWhitelist)
	}
	if len(effective.CommandBlacklist) != 0 {
		t.Fatalf("blacklist = %#v, want override to clear default", effective.CommandBlacklist)
	}
}

func TestHostAppend(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
command_whitelist = ["^journalctl", "^uptime$"]
command_blacklist = ["rm"]

[policy.hosts.prod]
command_whitelist = ["^uptime$", "^systemctl status"]
command_blacklist = ["shutdown"]
`)
	effective := checker.EffectivePolicy("prod")
	if got := strings.Join(effective.CommandWhitelist, "|"); got != "^journalctl|^uptime$|^systemctl status" {
		t.Fatalf("whitelist = %s", got)
	}
	if got := strings.Join(effective.CommandBlacklist, "|"); got != "rm|shutdown" {
		t.Fatalf("blacklist = %s", got)
	}
}

func TestHostDisabled(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
command_whitelist = ["^journalctl"]

[policy.hosts.prod]
enabled = false
`)
	decision := checker.CheckCommand("prod", "rm -rf /")
	if !decision.Allowed {
		t.Fatalf("decision = %#v, want allowed because host disabled", decision)
	}
}

func TestLocalPathEscape(t *testing.T) {
	root := t.TempDir()
	safe := filepath.Join(root, "safe")
	if err := os.Mkdir(safe, 0700); err != nil {
		t.Fatal(err)
	}
	checker := checkerFromTOML(t, fmt.Sprintf(`
[policy.default]
local_path_whitelist = [%q]
`, safe))

	decision := checker.CheckLocalPath("prod", filepath.Join(safe, "..", "evil"))
	if decision.Allowed || decision.Reason != policy.ReasonLocalPathDenied {
		t.Fatalf("decision = %#v, want local path denied", decision)
	}
}

func TestLocalPathPrefixTrap(t *testing.T) {
	root := t.TempDir()
	safe := filepath.Join(root, "a")
	trap := filepath.Join(root, "abc")
	if err := os.Mkdir(safe, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(trap, 0700); err != nil {
		t.Fatal(err)
	}
	checker := checkerFromTOML(t, fmt.Sprintf(`
[policy.default]
local_path_whitelist = [%q]
`, safe))

	decision := checker.CheckLocalPath("prod", filepath.Join(trap, "file.txt"))
	if decision.Allowed {
		t.Fatalf("decision = %#v, prefix trap should be denied", decision)
	}
}

func TestLocalPathSymlinkResolved(t *testing.T) {
	root := t.TempDir()
	safe := filepath.Join(root, "safe")
	outside := filepath.Join(root, "outside")
	link := filepath.Join(safe, "link")
	if err := os.Mkdir(safe, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	checker := checkerFromTOML(t, fmt.Sprintf(`
[policy.default]
local_path_whitelist = [%q]
`, safe))

	decision := checker.CheckLocalPath("prod", filepath.Join(link, "secret.txt"))
	if decision.Allowed {
		t.Fatalf("decision = %#v, symlink escape should be denied", decision)
	}
}

func TestRemotePathRelative(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
remote_path_whitelist = ["/var/log"]
`)
	decision := checker.CheckRemotePath("prod", "relative.log")
	if decision.Allowed || decision.Reason != policy.ReasonRemotePathDenied {
		t.Fatalf("decision = %#v, want relative remote path denied", decision)
	}
}

func TestRemotePathWindowsWhitelist(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
remote_path_whitelist = ['C:\Temp', '\\fileserver\share']
`)

	cases := []struct {
		name    string
		path    string
		allowed bool
	}{
		{"drive backslash within", `C:\Temp\file.txt`, true},
		{"drive forward slash within", "C:/Temp/file.txt", true},
		{"drive lowercase within", `c:\temp\file.txt`, false}, // path segment case-sensitive
		{"drive lowercase letter only", "c:/Temp/file.txt", true},
		{"drive sibling boundary", `C:\TempEvil\x`, false},
		{"different drive", `D:\Temp\x`, false},
		{"unc within", `\\fileserver\share\dir\f`, true},
		{"unc forward slash within", "//fileserver/share/dir/f", true},
		{"unc sibling boundary", `\\fileserver\shareEvil\x`, false},
		{"relative windows", `Temp\x`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := checker.CheckRemotePath("prod", tc.path)
			if decision.Allowed != tc.allowed {
				t.Fatalf("CheckRemotePath(%q).Allowed = %v, want %v (decision=%#v)",
					tc.path, decision.Allowed, tc.allowed, decision)
			}
		})
	}
}

func TestRemotePathWindowsTraversalDenied(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
remote_path_whitelist = ['C:\Temp']
`)
	// A .. escape out of the whitelisted Windows directory must be denied after
	// normalization, the same way POSIX traversal is.
	decision := checker.CheckRemotePath("prod", `C:\Temp\..\Windows\System32`)
	if decision.Allowed {
		t.Fatalf("decision = %#v, windows traversal escape should be denied", decision)
	}
}

func TestGrantSupplementWhitelist(t *testing.T) {
	grants := policy.NewGrantManager()
	if _, err := grants.Add("prod", policy.KindCommand, "^uptime$", time.Minute); err != nil {
		t.Fatal(err)
	}
	checker := checkerFromTOMLWithGrants(t, `
[policy.default]
command_whitelist = ["^journalctl"]
`, grants)

	decision := checker.CheckCommand("prod", "uptime")
	if !decision.Allowed {
		t.Fatalf("decision = %#v, grant should supplement whitelist", decision)
	}
}

func TestGrantNotOverrideBlacklist(t *testing.T) {
	grants := policy.NewGrantManager()
	if _, err := grants.Add("prod", policy.KindCommand, "^rm ", time.Minute); err != nil {
		t.Fatal(err)
	}
	checker := checkerFromTOMLWithGrants(t, `
[policy.default]
command_whitelist = ["^journalctl"]
command_blacklist = ["^rm "]
`, grants)

	decision := checker.CheckCommand("prod", "rm -rf /tmp/x")
	if decision.Allowed || decision.Reason != policy.ReasonBlacklistMatch {
		t.Fatalf("decision = %#v, grant must not override blacklist", decision)
	}
}

func TestGrantExpiry(t *testing.T) {
	grants := policy.NewGrantManager()
	if _, err := grants.Add("prod", policy.KindCommand, "^uptime$", 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if !grants.MatchCommand("prod", "uptime") {
		t.Fatal("grant should match before expiry")
	}
	time.Sleep(80 * time.Millisecond)
	if grants.MatchCommand("prod", "uptime") {
		t.Fatal("grant should not match after expiry")
	}
}

func TestGrantRevoke(t *testing.T) {
	grants := policy.NewGrantManager()
	grant, err := grants.Add("prod", policy.KindCommand, "^uptime$", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !grants.Revoke(grant.ID) {
		t.Fatal("expected revoke to remove grant")
	}
	if grants.MatchCommand("prod", "uptime") {
		t.Fatal("revoked grant should not match")
	}
}

func TestBlockedErrorFormat(t *testing.T) {
	err := (&policy.BlockedError{
		Alias:   "prod",
		Kind:    policy.KindCommand,
		Reason:  policy.ReasonBlacklistMatch,
		Pattern: "(?i)rm\\b",
		Input:   "rm -rf /tmp/x",
	}).ToOutputError()
	if !strings.Contains(err.Hint, "(?i)rm\\b") {
		t.Fatalf("hint = %q, want pattern", err.Hint)
	}
	if !strings.Contains(err.Action, "grants do not override blacklists") {
		t.Fatalf("action = %q", err.Action)
	}
	if err.Code != output.CodePolicyBlocked {
		t.Fatalf("error.code = %q, want %q", err.Code, output.CodePolicyBlocked)
	}
}

func checkerFromTOML(t *testing.T, body string) *policy.Checker {
	return checkerFromTOMLWithGrants(t, body, nil)
}

func checkerFromTOMLWithGrants(t *testing.T, body string, grants *policy.GrantManager) *policy.Checker {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := appconfig.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	return policy.NewChecker(cfg, grants)
}
