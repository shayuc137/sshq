package hostkey

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
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

func testSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestFetchConn(t *testing.T) {
	signer := testSigner(t)
	client, server := net.Pipe()
	t.Cleanup(func() { client.Close(); server.Close() })
	originalHandshake := fetchConnHandshake
	t.Cleanup(func() { fetchConnHandshake = originalHandshake })
	fetchConnHandshake = func(_ net.Conn, addr string, cfg *ssh.ClientConfig) error {
		return cfg.HostKeyCallback(addr, &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 22}, signer.PublicKey())
	}

	key, err := FetchConn(client, "host.example:22", time.Second)
	if err != nil {
		t.Fatalf("FetchConn: %v", err)
	}
	if !bytes.Equal(key.Marshal(), signer.PublicKey().Marshal()) {
		t.Fatalf("fetched key = %s, want %s", Fingerprint(key), Fingerprint(signer.PublicKey()))
	}
}

func TestFetchConnTimeout(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	start := time.Now()
	_, err := FetchConn(client, "host.example:22", 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected error from timed-out fetch")
	}
	// The close-on-timeout guard (needed for proxy-tunneled conns that reject
	// deadlines) surfaces as a closed-conn handshake error, not net.Error.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("FetchConn took %v, guard did not fire", elapsed)
	}
}

func TestCheckStates(t *testing.T) {
	addr := "192.0.2.10:22"
	remote := testSigner(t).PublicKey()
	other := testSigner(t).PublicKey()

	for _, tt := range []struct {
		name    string
		content string
		status  Status
	}{
		{name: "unknown", status: Unknown},
		{name: "trusted", content: knownHostLine(addr, remote), status: Trusted},
		{name: "mismatch", content: knownHostLine(addr, other), status: Mismatch},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "known_hosts")
			t.Setenv(KnownHostsPathEnv, path)
			if tt.content != "" {
				if err := os.WriteFile(path, []byte(tt.content+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := Check(addr, remote)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if result.Status != tt.status {
				t.Fatalf("status = %v, want %v", result.Status, tt.status)
			}
			if tt.status == Mismatch && len(result.Want) == 0 {
				t.Fatal("mismatch result missing known key")
			}
		})
	}
}

func TestLookupKeys(t *testing.T) {
	got := LookupKeys("target", "192.0.2.10", "2222")
	want := []string{"target", "192.0.2.10", "[192.0.2.10]:2222"}
	if len(got) != len(want) {
		t.Fatalf("LookupKeys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("LookupKeys = %v, want %v", got, want)
		}
	}
}

func knownHostLine(addr string, key ssh.PublicKey) string {
	return knownhosts.Line([]string{knownhosts.Normalize(addr)}, key)
}
