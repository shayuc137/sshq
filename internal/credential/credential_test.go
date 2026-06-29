package credential

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestSetAndGet(t *testing.T) {
	store := newTestStore(t)

	if err := store.Set("switch", "secret-password"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("switch")
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret-password" {
		t.Fatalf("password = %q, want secret-password", got)
	}
}

func TestDelete(t *testing.T) {
	store := newTestStore(t)
	if err := store.Set("router", "secret-password"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("router"); err != nil {
		t.Fatal(err)
	}

	_, err := store.Get("router")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete error = %v, want ErrNotFound", err)
	}
}

func TestList(t *testing.T) {
	store := newTestStore(t)
	if err := store.Set("zeta", "one"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("alpha", "two"); err != nil {
		t.Fatal(err)
	}

	got, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "zeta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("List = %v, want %v", got, want)
	}
}

func TestOverwrite(t *testing.T) {
	store := newTestStore(t)
	if err := store.Set("nas", "old-password"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("nas", "new-password"); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get("nas")
	if err != nil {
		t.Fatal(err)
	}
	if got != "new-password" {
		t.Fatalf("password = %q, want new-password", got)
	}
}

func TestFileNotExist(t *testing.T) {
	store, err := Open(WithPath(filepath.Join(t.TempDir(), "missing.age")), withKeyPaths())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing error = %v, want ErrNotFound", err)
	}
	if aliases, err := store.List(); err != nil || len(aliases) != 0 {
		t.Fatalf("List missing = %v, %v; want empty nil-error", aliases, err)
	}
	if err := store.Delete("missing"); err != nil {
		t.Fatalf("Delete missing returned %v", err)
	}
}

func TestPassphraseFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.age")
	store, err := Open(
		WithPath(path),
		withKeyPaths(),
		WithPassphrase(func() (string, error) { return "store-passphrase", nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("legacy", "password-only"); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(
		WithPath(path),
		withKeyPaths(),
		WithPassphrase(func() (string, error) { return "store-passphrase", nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if got != "password-only" {
		t.Fatalf("password = %q, want password-only", got)
	}
}

func TestAtomicWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory chmod semantics are platform-specific on Windows")
	}

	store := newTestStore(t)
	if err := store.Set("ap", "old-password"); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Dir(store.Path())
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)

	err := store.Set("ap", "new-password")
	if err == nil {
		t.Skip("directory permissions did not block writes on this filesystem")
	}

	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("ap")
	if err != nil {
		t.Fatal(err)
	}
	if got != "old-password" {
		t.Fatalf("password after failed write = %q, want old-password", got)
	}
}

func TestNoSensitiveInError(t *testing.T) {
	password := "never-log-this-password"
	store, err := Open(WithPath(filepath.Join(t.TempDir(), "credentials.age")), withKeyPaths())
	if err != nil {
		t.Fatal(err)
	}

	err = store.Set("host", password)
	if !errors.Is(err, ErrNoEncryptionKey) {
		t.Fatalf("Set error = %v, want ErrNoEncryptionKey", err)
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("error leaked password: %q", err.Error())
	}

	keyPath := writeTestSSHKey(t, t.TempDir())
	corruptPath := filepath.Join(t.TempDir(), "credentials.age")
	if err := os.WriteFile(corruptPath, []byte("not age: "+password), 0600); err != nil {
		t.Fatal(err)
	}
	corruptStore, err := Open(WithPath(corruptPath), withKeyPaths(keyPath))
	if err != nil {
		t.Fatal(err)
	}
	_, err = corruptStore.Get("host")
	if err == nil {
		t.Fatal("expected corrupt credential file error")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("corrupt-file error leaked password: %q", err.Error())
	}
}

func TestFilePermission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod is a no-op on Windows")
	}

	store := newTestStore(t)
	if err := store.Set("host", "password"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("credential file mode = %04o, want 0600", got)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	keyPath := writeTestSSHKey(t, dir)
	store, err := Open(WithPath(filepath.Join(dir, "credentials.age")), withKeyPaths(keyPath))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func writeTestSSHKey(t *testing.T, dir string) string {
	t.Helper()

	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "test")
	if err != nil {
		t.Fatal(err)
	}

	keyPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath+".pub", ssh.MarshalAuthorizedKey(signer.PublicKey()), 0644); err != nil {
		t.Fatal(err)
	}

	return keyPath
}
