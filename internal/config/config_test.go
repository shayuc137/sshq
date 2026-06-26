package config

import (
	"os"
	"path/filepath"
	"testing"
)

const testConfig = `# description: my server
# environment: production
Host myserver
    HostName 10.0.0.1
    User admin
    Port 2222
    IdentityFile ~/.ssh/mykey

Host dev-*
    User developer
    Port 9000

# sshq: shell=bash
# sshq: description=overridden desc
# description: old desc from legacy
Host tagged
    HostName 10.0.0.2
    IdentityFile ~/.ssh/tagged_key

Host *.example.com
    User webadmin

Host *
    User defaultuser
    Port 22
    Compression yes
`

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	hosts := store.List()
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d: %+v", len(hosts), hosts)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestGet(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	store, _ := Load(path)

	h, err := store.Get("myserver")
	if err != nil {
		t.Fatal(err)
	}
	if h.HostName != "10.0.0.1" {
		t.Errorf("HostName = %q, want %q", h.HostName, "10.0.0.1")
	}
	if h.User != "admin" {
		t.Errorf("User = %q, want %q", h.User, "admin")
	}
	if h.Port != "2222" {
		t.Errorf("Port = %q, want %q", h.Port, "2222")
	}
	if h.IdentityFile != "~/.ssh/mykey" {
		t.Errorf("IdentityFile = %q, want %q", h.IdentityFile, "~/.ssh/mykey")
	}
}

func TestGet_NotFound(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	store, _ := Load(path)

	_, err := store.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent host")
	}
}

func TestGlobalConfig(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	store, _ := Load(path)

	h, _ := store.Get("tagged")
	if h.User != "defaultuser" {
		t.Errorf("User should inherit from Host *, got %q", h.User)
	}
	if h.Port != "22" {
		t.Errorf("Port should inherit from Host *, got %q", h.Port)
	}
}

func TestWildcardSkipped(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	store, _ := Load(path)

	for _, h := range store.List() {
		if h.Alias == "*" || h.Alias == "dev-*" || h.Alias == "*.example.com" {
			t.Errorf("wildcard host %q should be excluded", h.Alias)
		}
	}
}

func TestMetadata_LegacyFormat(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	store, _ := Load(path)

	h, _ := store.Get("myserver")
	if h.Metadata["description"] != "my server" {
		t.Errorf("description = %q, want %q", h.Metadata["description"], "my server")
	}
	if h.Metadata["environment"] != "production" {
		t.Errorf("environment = %q, want %q", h.Metadata["environment"], "production")
	}
}

func TestMetadata_SshqFormat(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	store, _ := Load(path)

	h, _ := store.Get("tagged")
	if h.Metadata["description"] != "overridden desc" {
		t.Errorf("sshq format should override legacy, got %q", h.Metadata["description"])
	}
	if h.Metadata["shell"] != "bash" {
		t.Errorf("shell = %q, want %q", h.Metadata["shell"], "bash")
	}
}

func TestSearch(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	store, _ := Load(path)

	results := store.Search("server")
	if len(results) != 1 || results[0].Alias != "myserver" {
		t.Errorf("search 'server' = %+v, want [myserver]", results)
	}
}

func TestSearch_ByDescription(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	store, _ := Load(path)

	results := store.Search("overridden")
	if len(results) != 1 || results[0].Alias != "tagged" {
		t.Errorf("search by description = %+v, want [tagged]", results)
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	store, _ := Load(path)

	results := store.Search("MYSERVER")
	if len(results) != 1 {
		t.Errorf("case-insensitive search should match, got %d", len(results))
	}
}

func TestSearch_NoMatch(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	store, _ := Load(path)

	results := store.Search("zzzzz")
	if len(results) != 0 {
		t.Errorf("expected 0 matches, got %d", len(results))
	}
}

func TestLoadDefault_EnvVar(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	t.Setenv("SSHQ_CONFIG", path)

	store, err := LoadDefault("")
	if err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 2 {
		t.Errorf("expected 2 hosts via env var, got %d", len(store.List()))
	}
}

func TestLoadDefault_FlagOverridesEnv(t *testing.T) {
	path := writeTestConfig(t, testConfig)
	t.Setenv("SSHQ_CONFIG", "/nonexistent")

	store, err := LoadDefault(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 2 {
		t.Errorf("flag should override env var, got %d hosts", len(store.List()))
	}
}
