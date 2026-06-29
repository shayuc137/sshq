package cli

import (
	"os"
	"strings"
	"testing"
)

// envUnset removes key for the duration of the test and restores any prior
// value on cleanup. Used because testing.T has no Unsetenv and the providers
// distinguish "unset" from "set to empty" via os.LookupEnv.
func envUnset(t *testing.T, key string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			os.Setenv(key, prev)
		} else {
			os.Unsetenv(key)
		}
	})
}

// TestRuntimePassphraseProviderEnv verifies M1: the runtime provider reads the
// passphrase from SSHQ_CREDENTIAL_PASSPHRASE so passphrase-mode stores are
// usable headlessly (daemon/agent/exec without a TTY).
func TestRuntimePassphraseProviderEnv(t *testing.T) {
	t.Setenv(EnvCredentialPassphrase, "s3cr3t")
	provider := runtimePassphraseProvider(nil)

	got, err := provider()
	if err != nil {
		t.Fatalf("provider err = %v, want nil", err)
	}
	if got != "s3cr3t" {
		t.Fatalf("passphrase = %q, want s3cr3t", got)
	}

	// Cached: a second call returns the same value without re-reading.
	if got2, _ := provider(); got2 != "s3cr3t" {
		t.Fatalf("second call = %q, want s3cr3t", got2)
	}
}

// TestRuntimePassphraseProviderNoTTYNoEnv: without env and without a TTY the
// provider returns a clear error instead of silently yielding an empty
// passphrase (which previously made passphrase-mode creds fail as generic auth).
func TestRuntimePassphraseProviderNoTTYNoEnv(t *testing.T) {
	// Ensure the env var is not set in this test's environment.
	envUnset(t, EnvCredentialPassphrase)

	provider := runtimePassphraseProvider(nil)
	_, err := provider()
	if err == nil {
		t.Fatal("expected error when no env and no TTY, got nil")
	}
	if !strings.Contains(err.Error(), EnvCredentialPassphrase) {
		t.Fatalf("error = %v, want mention of %s", err, EnvCredentialPassphrase)
	}
}

// TestDaemonPassphraseProvider: the daemon provider is env-only.
func TestDaemonPassphraseProvider(t *testing.T) {
	t.Setenv(EnvCredentialPassphrase, "daemonpass")
	if got, err := daemonPassphraseProvider()(); err != nil || got != "daemonpass" {
		t.Fatalf("daemon provider = (%q, %v), want (daemonpass, nil)", got, err)
	}

	envUnset(t, EnvCredentialPassphrase)
	if _, err := daemonPassphraseProvider()(); err == nil {
		t.Fatal("daemon provider without env returned nil error, want error")
	}
}
