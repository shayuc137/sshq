package policy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shayuc137/sshq/internal/appconfig"
	"github.com/shayuc137/sshq/internal/policy"
)

func TestLocalForwardExactMatch(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
local_forward_whitelist = ["localhost:8080"]
`)
	d := checker.CheckLocalForward("prod", "localhost:8080")
	if !d.Allowed {
		t.Fatalf("expected allowed, got %#v", d)
	}

	d = checker.CheckLocalForward("prod", "localhost:9090")
	if d.Allowed || d.Reason != policy.ReasonForwardDenied {
		t.Fatalf("expected forward denied, got %#v", d)
	}
}

func TestLocalForwardPortWildcard(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
local_forward_whitelist = ["localhost:*"]
`)
	for _, port := range []string{"0", "80", "8080", "65535"} {
		d := checker.CheckLocalForward("prod", "localhost:"+port)
		if !d.Allowed {
			t.Fatalf("port %s should be allowed, got %#v", port, d)
		}
	}

	d := checker.CheckLocalForward("prod", "10.0.0.1:8080")
	if d.Allowed {
		t.Fatalf("different host should be denied, got %#v", d)
	}
}

func TestLocalForwardPortRange(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
local_forward_whitelist = ["localhost:8000-9000"]
`)
	cases := []struct {
		target  string
		allowed bool
	}{
		{"localhost:7999", false},
		{"localhost:8000", true},
		{"localhost:8500", true},
		{"localhost:9000", true},
		{"localhost:9001", false},
	}
	for _, tc := range cases {
		d := checker.CheckLocalForward("prod", tc.target)
		if d.Allowed != tc.allowed {
			t.Fatalf("target=%s expected allowed=%v, got %#v", tc.target, tc.allowed, d)
		}
	}
}

func TestLocalForwardHostWildcard(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
local_forward_whitelist = ["*:22"]
`)
	d := checker.CheckLocalForward("prod", "anything:22")
	if !d.Allowed {
		t.Fatalf("wildcard host should match, got %#v", d)
	}
	d = checker.CheckLocalForward("prod", "anything:23")
	if d.Allowed {
		t.Fatalf("port 23 should not match, got %#v", d)
	}
}

func TestLocalForwardFullWildcard(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
local_forward_whitelist = ["*:*"]
`)
	d := checker.CheckLocalForward("prod", "any-host:12345")
	if !d.Allowed {
		t.Fatalf("full wildcard should match all, got %#v", d)
	}
}

func TestForwardEmptyWhitelistAllowsAll(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
local_forward_whitelist = []
`)
	d := checker.CheckLocalForward("prod", "anything:1234")
	if !d.Allowed {
		t.Fatalf("empty whitelist should allow all, got %#v", d)
	}
}

func TestForwardNoWhitelistAllowsAll(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
command_whitelist = ["^uptime$"]
`)
	d := checker.CheckLocalForward("prod", "anything:1234")
	if !d.Allowed {
		t.Fatalf("missing whitelist should allow all, got %#v", d)
	}
}

func TestRemoteForwardCheck(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
remote_forward_whitelist = ["localhost:3000"]
`)
	d := checker.CheckRemoteForward("prod", "localhost:3000")
	if !d.Allowed {
		t.Fatalf("expected allowed, got %#v", d)
	}
	d = checker.CheckRemoteForward("prod", "localhost:4000")
	if d.Allowed || d.Reason != policy.ReasonForwardDenied {
		t.Fatalf("expected forward denied, got %#v", d)
	}
}

func TestForwardHostCaseInsensitive(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
local_forward_whitelist = ["LocalHost:8080"]
`)
	d := checker.CheckLocalForward("prod", "localhost:8080")
	if !d.Allowed {
		t.Fatalf("host match should be case-insensitive, got %#v", d)
	}
}

func TestForwardPerHostOverride(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
local_forward_whitelist = ["localhost:*"]

[policy.hosts.prod]
mode = "override"
local_forward_whitelist = ["localhost:8080"]
`)
	d := checker.CheckLocalForward("prod", "localhost:9090")
	if d.Allowed {
		t.Fatalf("per-host override should restrict, got %#v", d)
	}
	d = checker.CheckLocalForward("prod", "localhost:8080")
	if !d.Allowed {
		t.Fatalf("per-host override should allow exact, got %#v", d)
	}
	d = checker.CheckLocalForward("other", "localhost:9090")
	if !d.Allowed {
		t.Fatalf("non-overridden host should use default, got %#v", d)
	}
}

func TestForwardPerHostAppend(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
local_forward_whitelist = ["localhost:8080"]

[policy.hosts.prod]
local_forward_whitelist = ["localhost:9090"]
`)
	effective := checker.EffectivePolicy("prod")
	if got := strings.Join(effective.LocalForwardWhitelist, "|"); got != "localhost:8080|localhost:9090" {
		t.Fatalf("forward whitelist = %s, want appended", got)
	}
}

func TestForwardPolicyDisabled(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
enabled = false
local_forward_whitelist = ["localhost:8080"]
`)
	d := checker.CheckLocalForward("prod", "anything:1234")
	if !d.Allowed {
		t.Fatalf("disabled policy should allow all, got %#v", d)
	}
}

func TestForwardGrantFallback(t *testing.T) {
	grants := policy.NewGrantManager()
	if _, err := grants.Add("prod", policy.KindLocalForward, "db-server:5432", time.Minute); err != nil {
		t.Fatal(err)
	}
	checker := checkerFromTOMLWithGrants(t, `
[policy.default]
local_forward_whitelist = ["localhost:8080"]
`, grants)

	d := checker.CheckLocalForward("prod", "db-server:5432")
	if !d.Allowed {
		t.Fatalf("grant should supplement forward whitelist, got %#v", d)
	}
}

func TestForwardGrantRemoteForward(t *testing.T) {
	grants := policy.NewGrantManager()
	if _, err := grants.Add("prod", policy.KindRemoteForward, "localhost:4000", time.Minute); err != nil {
		t.Fatal(err)
	}
	checker := checkerFromTOMLWithGrants(t, `
[policy.default]
remote_forward_whitelist = ["localhost:3000"]
`, grants)

	d := checker.CheckRemoteForward("prod", "localhost:4000")
	if !d.Allowed {
		t.Fatalf("remote forward grant should supplement whitelist, got %#v", d)
	}
}

func TestForwardBlockedErrorMessage(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
local_forward_whitelist = ["localhost:8080"]
`)
	d := checker.CheckLocalForward("prod", "db:5432")
	if d.Allowed {
		t.Fatal("expected blocked")
	}
	err := d.Err()
	if err == nil {
		t.Fatal("expected error")
	}
	be, ok := err.(*policy.BlockedError)
	if !ok {
		t.Fatalf("expected *BlockedError, got %T", err)
	}
	msg := be.Error()
	if !strings.Contains(msg, "forward blocked") {
		t.Fatalf("error message should mention forward blocked, got %q", msg)
	}
	if !strings.Contains(msg, "local-forward") {
		t.Fatalf("error action should mention local-forward kind, got %q", msg)
	}
}

func TestForwardInvalidEntry(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
local_forward_whitelist = ["badentry"]
`)
	d := checker.CheckLocalForward("prod", "localhost:8080")
	if d.Allowed || d.Reason != policy.ReasonConfigError {
		t.Fatalf("invalid entry should cause config error, got %#v", d)
	}
}

func TestForwardValidateConfig(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
local_forward_whitelist = ["localhost:8080", "localhost:*", "*:22", "localhost:8000-9000"]
remote_forward_whitelist = ["*:*"]
`)
	_ = checker

	errs := validateForwardConfig(t, `
[policy.default]
local_forward_whitelist = ["badentry", "localhost:99999"]
`)
	if len(errs) != 2 {
		t.Fatalf("expected 2 validation errors, got %d: %v", len(errs), errs)
	}
}

func validateForwardConfig(t *testing.T, body string) []policy.ValidationError {
	t.Helper()
	cfg := loadTOMLConfig(t, body)
	return policy.ValidateConfig(cfg, nil)
}

func loadTOMLConfig(t *testing.T, body string) *appconfig.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := appconfig.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestForwardPortEdgeCases(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
local_forward_whitelist = ["localhost:0", "localhost:65535"]
`)
	d := checker.CheckLocalForward("prod", "localhost:0")
	if !d.Allowed {
		t.Fatalf("port 0 should be allowed, got %#v", d)
	}
	d = checker.CheckLocalForward("prod", "localhost:65535")
	if !d.Allowed {
		t.Fatalf("port 65535 should be allowed, got %#v", d)
	}
}

func TestForwardMultipleEntries(t *testing.T) {
	checker := checkerFromTOML(t, `
[policy.default]
local_forward_whitelist = ["localhost:8080", "10.0.0.1:5432", "*:22"]
`)
	cases := []struct {
		target  string
		allowed bool
	}{
		{"localhost:8080", true},
		{"10.0.0.1:5432", true},
		{"any-host:22", true},
		{"10.0.0.1:3306", false},
		{"localhost:9090", false},
	}
	for _, tc := range cases {
		d := checker.CheckLocalForward("prod", tc.target)
		if d.Allowed != tc.allowed {
			t.Fatalf("target=%s expected allowed=%v, got %#v", tc.target, tc.allowed, d)
		}
	}
}
