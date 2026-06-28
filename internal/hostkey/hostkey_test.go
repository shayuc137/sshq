package hostkey

import (
	"path/filepath"
	"testing"
)

func TestPathUsesEnvOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	t.Setenv(KnownHostsPathEnv, path)

	got, err := Path()
	if err != nil {
		t.Fatalf("Path returned error: %v", err)
	}
	if got != path {
		t.Fatalf("Path = %q, want %q", got, path)
	}
}
