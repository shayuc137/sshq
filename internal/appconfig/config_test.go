package appconfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadEmpty(t *testing.T) {
	cfg, err := LoadFrom(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Exists() {
		t.Fatal("missing config should not be marked as existing")
	}
	if cfg.Policy.Hosts != nil {
		t.Fatalf("hosts = %#v, want nil for zero config", cfg.Policy.Hosts)
	}
}

func TestLoadValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeConfig(t, path, `
[policy.default]
enabled = true
command_whitelist = ["^journalctl(\\s|$)"]
command_blacklist = ["(?i)rm\\b"]
local_path_whitelist = ["~/safe"]
remote_path_whitelist = ["/var/log"]

[policy.hosts.prod]
enabled = false
mode = "override"
command_whitelist = ["^uptime$"]

[audit]
enabled = true
path = "/tmp/audit.log"
max_size = "10MiB"
`)

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Exists() {
		t.Fatal("config should be marked as existing")
	}
	if cfg.Policy.Default.Enabled == nil || !*cfg.Policy.Default.Enabled {
		t.Fatalf("default enabled not decoded")
	}
	if got := cfg.Policy.Default.CommandWhitelist[0]; got != `^journalctl(\s|$)` {
		t.Fatalf("command whitelist = %q", got)
	}
	host := cfg.Policy.Hosts["prod"]
	if host.Enabled == nil || *host.Enabled {
		t.Fatalf("host enabled = %#v, want false", host.Enabled)
	}
	if host.Mode != "override" {
		t.Fatalf("host mode = %q", host.Mode)
	}
	if cfg.Audit.Enabled == nil || !*cfg.Audit.Enabled {
		t.Fatalf("audit enabled not decoded")
	}
	if cfg.Audit.MaxSize != "10MiB" {
		t.Fatalf("audit max size = %q", cfg.Audit.MaxSize)
	}
}

func TestLoadStrictFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeConfig(t, path, `
[policy.default]
command_whitelst = ["typo"]
`)

	if _, err := LoadFrom(path); err == nil {
		t.Fatal("expected strict decode error")
	}
}

func TestReloadIfChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeConfig(t, path, `
[policy.default]
command_whitelist = ["^one$"]
`)

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}

	changed, err := cfg.ReloadIfChanged()
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("unchanged config should not reload")
	}

	writeConfig(t, path, `
[policy.default]
command_whitelist = ["^two$"]
`)
	forced := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, forced, forced); err != nil {
		t.Fatal(err)
	}

	changed, err = cfg.ReloadIfChanged()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed config should reload")
	}
	if got := cfg.Policy.Default.CommandWhitelist[0]; got != "^two$" {
		t.Fatalf("whitelist = %q", got)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	changed, err = cfg.ReloadIfChanged()
	if err != nil {
		t.Fatal(err)
	}
	if !changed || cfg.Exists() {
		t.Fatalf("removed config should reload to empty, changed=%v exists=%v", changed, cfg.Exists())
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"10MB", 10_000_000},
		{"100MiB", 100 * 1024 * 1024},
		{"1048576", 1_048_576},
		{"1.5KB", 1500},
	}
	for _, tt := range tests {
		got, err := ParseSize(tt.in)
		if err != nil {
			t.Fatalf("ParseSize(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("ParseSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}

	if _, err := ParseSize("10XB"); err == nil {
		t.Fatal("expected invalid unit error")
	}
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
