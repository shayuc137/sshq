package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/hostkey"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/sshclient"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestTrustAllPromotesResultsInJSONAndPreservesPrettyInfo(t *testing.T) {
	store := trustStoreForTest(t)
	originalDial := dialTrustTCP
	originalFetch := fetchTrustHostKey
	t.Cleanup(func() { dialTrustTCP = originalDial; fetchTrustHostKey = originalFetch })
	t.Setenv(hostkey.KnownHostsPathEnv, filepath.Join(t.TempDir(), "known_hosts"))
	signer := cliTestSigner(t)
	fetchTrustHostKey = func(net.Conn, string, time.Duration) (ssh.PublicKey, error) {
		return signer.PublicKey(), nil
	}
	dialTrustTCP = func(_ context.Context, cfg sshclient.ConnConfig) (net.Conn, io.Closer, error) {
		if cfg.Alias == "target" {
			return nil, nil, errors.New("connection refused")
		}
		conn, closer := trustPipe(t)
		return conn, closer, nil
	}

	var jsonOut, jsonErr bytes.Buffer
	err := trustAll(context.Background(), output.New(&jsonOut, &jsonErr, output.WithJSON()), store, nil, false, time.Second)
	assertBadNews(t, err)
	if jsonErr.Len() != 0 {
		t.Fatalf("JSON stderr = %q, want empty", jsonErr.String())
	}
	var envelope struct {
		Data struct {
			Results []trustAllHostResult `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(jsonOut.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, jsonOut.String())
	}
	if len(envelope.Data.Results) != len(store.List()) {
		t.Fatalf("results = %+v", envelope.Data.Results)
	}
	failed := 0
	added := 0
	for _, result := range envelope.Data.Results {
		switch result.Status {
		case "failed":
			failed++
			if result.Alias != "target" || result.Error != "connection refused" {
				t.Fatalf("failed result = %+v", result)
			}
		case "added":
			added++
			if result.Error != "" {
				t.Fatalf("added result = %+v", result)
			}
		}
	}
	if failed != 1 || added != 2 {
		t.Fatalf("results = %+v", envelope.Data.Results)
	}

	var prettyOut, prettyErr bytes.Buffer
	err = trustAll(context.Background(), output.New(&prettyOut, &prettyErr, output.WithPretty()), store, nil, false, time.Second)
	assertBadNews(t, err)
	if !strings.Contains(prettyOut.String(), "total=3 trusted=2 added=0 failed=1") {
		t.Fatalf("pretty stdout = %q", prettyOut.String())
	}
	if !strings.Contains(prettyErr.String(), "target (192.0.2.12:2222) unreachable: connection refused") {
		t.Fatalf("pretty stderr = %q", prettyErr.String())
	}
}

func trustStoreForTest(t *testing.T) *config.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	raw := "Host jump\n  HostName 192.0.2.10\n  User jump-user\n" +
		"Host direct\n  HostName 192.0.2.11\n  User direct-user\n  Port 2222\n" +
		"Host target\n  HostName 192.0.2.12\n  User target-user\n  Port 2222\n  ProxyJump jump\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func cliTestSigner(t *testing.T) ssh.Signer {
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

func trustPipe(t *testing.T) (net.Conn, io.Closer) {
	t.Helper()
	client, peer := net.Pipe()
	t.Cleanup(func() { peer.Close() })
	return client, client
}

func TestTrustHostUsesDirectAndProxyPaths(t *testing.T) {
	store := trustStoreForTest(t)
	originalDial := dialTrustTCP
	originalFetch := fetchTrustHostKey
	t.Cleanup(func() { dialTrustTCP = originalDial; fetchTrustHostKey = originalFetch })

	for _, tt := range []struct {
		alias     string
		wantProxy bool
	}{
		{alias: "direct"},
		{alias: "target", wantProxy: true},
	} {
		t.Run(tt.alias, func(t *testing.T) {
			knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
			t.Setenv(hostkey.KnownHostsPathEnv, knownHostsPath)
			signer := cliTestSigner(t)
			fetchTrustHostKey = func(net.Conn, string, time.Duration) (ssh.PublicKey, error) {
				return signer.PublicKey(), nil
			}
			dialTrustTCP = func(_ context.Context, cfg sshclient.ConnConfig) (net.Conn, io.Closer, error) {
				if tt.wantProxy {
					if cfg.ProxyJump != "jump" || cfg.ProxyConfig == nil || cfg.ProxyConfig.Alias != "jump" {
						t.Fatalf("proxy config = %+v", cfg)
					}
				} else if cfg.ProxyJump != "" || cfg.ProxyConfig != nil {
					t.Fatalf("direct config = %+v", cfg)
				}
				conn, closer := trustPipe(t)
				return conn, closer, nil
			}

			result := trustHost(context.Background(), store, nil, tt.alias, false, time.Second)
			if result.Outcome != trustAdded || result.Status != "added" {
				t.Fatalf("trust result = %+v", result)
			}
			wantPath := ""
			if tt.wantProxy {
				wantPath = "jump"
			}
			if result.ProxyJump != wantPath || result.LookupAlias != tt.alias || len(result.LookupKeys) != 3 {
				t.Fatalf("path metadata = %+v", result)
			}
			if result.RemoteFingerprint != hostkey.Fingerprint(signer.PublicKey()) {
				t.Fatalf("remote fingerprint = %q", result.RemoteFingerprint)
			}
		})
	}
}

func TestTrustHostThreeStatesAndReplace(t *testing.T) {
	store := trustStoreForTest(t)
	path := filepath.Join(t.TempDir(), "known_hosts")
	t.Setenv(hostkey.KnownHostsPathEnv, path)
	remoteSigner := cliTestSigner(t)
	oldSigner := cliTestSigner(t)
	addr := "192.0.2.12:2222"
	oldLine := knownhosts.Line([]string{knownhosts.Normalize(addr)}, oldSigner.PublicKey())
	if err := os.WriteFile(path, []byte(oldLine+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	originalDial := dialTrustTCP
	originalFetch := fetchTrustHostKey
	t.Cleanup(func() { dialTrustTCP = originalDial; fetchTrustHostKey = originalFetch })
	fetchTrustHostKey = func(net.Conn, string, time.Duration) (ssh.PublicKey, error) {
		return remoteSigner.PublicKey(), nil
	}
	dialTrustTCP = func(context.Context, sshclient.ConnConfig) (net.Conn, io.Closer, error) {
		conn, closer := trustPipe(t)
		return conn, closer, nil
	}

	mismatch := trustHost(context.Background(), store, nil, "target", false, time.Second)
	if mismatch.Outcome != trustMismatch || mismatch.Status != "mismatch" {
		t.Fatalf("mismatch result = %+v", mismatch)
	}
	if mismatch.RemoteFingerprint != hostkey.Fingerprint(remoteSigner.PublicKey()) ||
		mismatch.KnownFingerprint != hostkey.Fingerprint(oldSigner.PublicKey()) {
		t.Fatalf("mismatch fingerprints = %+v", mismatch)
	}
	w := output.New(&bytes.Buffer{}, &bytes.Buffer{}, output.WithJSON())
	err := renderTrustOne(w, mismatch)
	cmdErr, ok := err.(*output.CmdError)
	if !ok {
		t.Fatalf("render mismatch error = %T", err)
	}
	if cmdErr.Action != "run: sshq trust target --replace" || cmdErr.Details["known_fingerprint"] == "" {
		t.Fatalf("mismatch error = %+v", cmdErr)
	}

	replaced := trustHost(context.Background(), store, nil, "target", true, time.Second)
	if replaced.Outcome != trustReplaced || replaced.Status != "replaced" {
		t.Fatalf("replace result = %+v", replaced)
	}
	trusted := trustHost(context.Background(), store, nil, "target", false, time.Second)
	if trusted.Outcome != trustAlready || trusted.Status != "trusted" {
		t.Fatalf("trusted result = %+v", trusted)
	}
	if trusted.KnownFingerprint != trusted.RemoteFingerprint {
		t.Fatalf("trusted fingerprints = %+v", trusted)
	}

	b, err := json.Marshal(trusted)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"target", "hostname", "port", "proxy_jump", "lookup_alias", "lookup_keys", "remote_fingerprint", "known_fingerprint"} {
		if _, ok := data[field]; !ok {
			t.Errorf("trust JSON missing %q: %s", field, b)
		}
	}
}
