package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shayuc137/sshq/internal/config"
)

func TestDaemonReloadSSHConfigReplacesStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("Host first\n  HostName 192.0.2.1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	dc := &daemonContext{store: store, storePath: store.Path()}
	dc.recordStoreState()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("Host second\n  HostName 192.0.2.2\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	dc.reloadSSHConfig()
	host, err := dc.storeSnapshot().Get("second")
	if err != nil {
		t.Fatalf("reloaded store does not contain appended host: %v", err)
	}
	if host.HostName != "192.0.2.2" {
		t.Fatalf("hostname = %q, want 192.0.2.2", host.HostName)
	}
}
