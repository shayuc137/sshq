package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreAdd(t *testing.T) {
	raw := []byte("Host existing\n    HostName 1.1.1.1\n")
	store, err := loadFromBytes(raw, "")
	if err != nil {
		t.Fatal(err)
	}

	err = store.Add(Host{
		Alias:    "newhost",
		HostName: "2.2.2.2",
		User:     "admin",
		Port:     "2222",
	})
	if err != nil {
		t.Fatal(err)
	}

	h, err := store.Get("newhost")
	if err != nil {
		t.Fatal("added host not found")
	}
	if h.HostName != "2.2.2.2" || h.User != "admin" || h.Port != "2222" {
		t.Errorf("unexpected host: %+v", h)
	}

	if _, err := store.Get("existing"); err != nil {
		t.Error("existing host lost after add")
	}
}

func TestStoreAddDuplicate(t *testing.T) {
	raw := []byte("Host myhost\n    HostName 1.1.1.1\n")
	store, err := loadFromBytes(raw, "")
	if err != nil {
		t.Fatal(err)
	}

	err = store.Add(Host{Alias: "myhost", HostName: "2.2.2.2"})
	if err == nil {
		t.Error("expected error for duplicate alias")
	}
}

func TestStoreAddWithMetadata(t *testing.T) {
	raw := []byte("Host existing\n    HostName 1.1.1.1\n")
	store, err := loadFromBytes(raw, "")
	if err != nil {
		t.Fatal(err)
	}

	err = store.Add(Host{
		Alias:    "tagged",
		HostName: "3.3.3.3",
		Metadata: map[string]string{"tags": "prod,web", "env": "production"},
	})
	if err != nil {
		t.Fatal(err)
	}

	h, err := store.Get("tagged")
	if err != nil {
		t.Fatal(err)
	}
	if h.Metadata["tags"] != "prod,web" {
		t.Errorf("tags = %q, want prod,web", h.Metadata["tags"])
	}
	if h.Metadata["env"] != "production" {
		t.Errorf("env = %q, want production", h.Metadata["env"])
	}
}

func TestStoreRemove(t *testing.T) {
	raw := []byte("Host first\n    HostName 1.1.1.1\n\nHost second\n    HostName 2.2.2.2\n")
	store, err := loadFromBytes(raw, "")
	if err != nil {
		t.Fatal(err)
	}

	err = store.Remove("first")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Get("first"); err == nil {
		t.Error("removed host still exists")
	}
	if _, err := store.Get("second"); err != nil {
		t.Error("other host lost after remove")
	}
}

func TestStoreRemoveNonexistent(t *testing.T) {
	raw := []byte("Host myhost\n    HostName 1.1.1.1\n")
	store, err := loadFromBytes(raw, "")
	if err != nil {
		t.Fatal(err)
	}

	err = store.Remove("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent host")
	}
}

func TestStoreRemoveWithMetadata(t *testing.T) {
	raw := []byte("# ===== tagged =====\n# sshq:tags=prod\nHost tagged\n    HostName 1.1.1.1\n\nHost other\n    HostName 2.2.2.2\n")
	store, err := loadFromBytes(raw, "")
	if err != nil {
		t.Fatal(err)
	}

	err = store.Remove("tagged")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(store.raw), "tagged") {
		t.Error("metadata comments not cleaned up")
	}
	if _, err := store.Get("other"); err != nil {
		t.Error("other host lost")
	}
}

func TestStoreSetSSHProperty(t *testing.T) {
	raw := []byte("Host myhost\n    HostName 1.1.1.1\n    User olduser\n")
	store, err := loadFromBytes(raw, "")
	if err != nil {
		t.Fatal(err)
	}

	err = store.Set("myhost", "User", "newuser")
	if err != nil {
		t.Fatal(err)
	}

	h, err := store.Get("myhost")
	if err != nil {
		t.Fatal(err)
	}
	if h.User != "newuser" {
		t.Errorf("user = %q, want newuser", h.User)
	}
}

func TestStoreSetNewSSHProperty(t *testing.T) {
	raw := []byte("Host myhost\n    HostName 1.1.1.1\n")
	store, err := loadFromBytes(raw, "")
	if err != nil {
		t.Fatal(err)
	}

	err = store.Set("myhost", "Port", "2222")
	if err != nil {
		t.Fatal(err)
	}

	h, err := store.Get("myhost")
	if err != nil {
		t.Fatal(err)
	}
	if h.Port != "2222" {
		t.Errorf("port = %q, want 2222", h.Port)
	}
}

func TestStoreSetMetadata(t *testing.T) {
	raw := []byte("Host myhost\n    HostName 1.1.1.1\n")
	store, err := loadFromBytes(raw, "")
	if err != nil {
		t.Fatal(err)
	}

	err = store.Set("myhost", "tags", "prod,web")
	if err != nil {
		t.Fatal(err)
	}

	h, err := store.Get("myhost")
	if err != nil {
		t.Fatal(err)
	}
	if h.Metadata["tags"] != "prod,web" {
		t.Errorf("tags = %q, want prod,web", h.Metadata["tags"])
	}
}

func TestStoreSetMetadataUpdate(t *testing.T) {
	raw := []byte("# sshq:tags=old\nHost myhost\n    HostName 1.1.1.1\n")
	store, err := loadFromBytes(raw, "")
	if err != nil {
		t.Fatal(err)
	}

	err = store.Set("myhost", "tags", "new,updated")
	if err != nil {
		t.Fatal(err)
	}

	h, err := store.Get("myhost")
	if err != nil {
		t.Fatal(err)
	}
	if h.Metadata["tags"] != "new,updated" {
		t.Errorf("tags = %q, want new,updated", h.Metadata["tags"])
	}
}

func TestStoreSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	os.WriteFile(path, []byte("Host myhost\n    HostName 1.1.1.1\n"), 0600)

	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	store.Add(Host{Alias: "newhost", HostName: "2.2.2.2"})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatal("saved file not parseable: " + err.Error())
	}
	if _, err := reloaded.Get("newhost"); err != nil {
		t.Error("saved host not found on reload")
	}
	if _, err := reloaded.Get("myhost"); err != nil {
		t.Error("original host lost on reload")
	}

	bak := path + ".bak"
	if _, err := os.Stat(bak); err != nil {
		t.Error("backup file not created")
	}
}

func TestCanonicalSSHKey(t *testing.T) {
	tests := map[string]string{
		"hostname":     "HostName",
		"HostName":     "HostName",
		"HOSTNAME":     "HostName",
		"user":         "User",
		"port":         "Port",
		"identityfile": "IdentityFile",
		"proxyjump":    "ProxyJump",
		"unknown":      "unknown",
	}
	for input, want := range tests {
		if got := canonicalSSHKey(input); got != want {
			t.Errorf("canonicalSSHKey(%q) = %q, want %q", input, got, want)
		}
	}
}
