package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/credential"
)

// TestHostToConnConfigCredentialErrorSurfaced verifies M2: a real
// credential-store error (here a corrupt file) is returned instead of being
// swallowed into a passwordless config that would later look like a generic
// auth failure.
func TestHostToConnConfigCredentialErrorSurfaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.age")
	if err := os.WriteFile(path, []byte("this is not an age file"), 0600); err != nil {
		t.Fatal(err)
	}
	creds, err := credential.Open(credential.WithPath(path))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	host := config.Host{Alias: "prod", HostName: "192.0.2.10", Port: "22", User: "tester"}
	_, err = hostToConnConfigWithCredentials(host, nil, creds)
	if err == nil {
		t.Fatal("expected credential error to be surfaced, got nil")
	}
	if errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("error should not be ErrNotFound: %v", err)
	}

	// The mapped summary must stay password-free and human-readable.
	summary := credentialErrorSummary(err)
	if summary == "" {
		t.Fatal("credentialErrorSummary returned empty string")
	}
	if strings.Contains(summary, "age file") {
		// sanity: summary should be a classification, not raw file bytes
		t.Fatalf("unexpected summary leaking input: %q", summary)
	}
}

// TestHostToConnConfigMissingCredentialIgnored verifies the ErrNotFound case:
// a store with no entry for the alias yields an empty password and no error.
func TestHostToConnConfigMissingCredentialIgnored(t *testing.T) {
	// Non-existent file => store reads as empty => Get returns ErrNotFound.
	path := filepath.Join(t.TempDir(), "missing.age")
	creds, err := credential.Open(credential.WithPath(path))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	host := config.Host{Alias: "prod", HostName: "192.0.2.10", Port: "22", User: "tester"}
	cfg, err := hostToConnConfigWithCredentials(host, nil, creds)
	if err != nil {
		t.Fatalf("missing credential should not error, got: %v", err)
	}
	if cfg.Password != "" {
		t.Fatalf("password = %q, want empty for missing credential", cfg.Password)
	}
	if cfg.Host != "192.0.2.10" || cfg.User != "tester" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}
