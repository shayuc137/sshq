package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/probe"
	"github.com/shayuc137/sshq/internal/sshclient"
)

func probeStoreForTest(t *testing.T) *config.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	raw := "Host jump\n  HostName 192.0.2.10\n  User jump-user\n" +
		"Host target\n  HostName 10.0.0.20\n  User target-user\n  Port 2222\n  ProxyJump jump\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestProbeUnreachableRendersDataAndReturnsBadNews(t *testing.T) {
	store := probeStoreForTest(t)
	originalDial := dialProbeTCP
	t.Cleanup(func() { dialProbeTCP = originalDial })
	dialProbeTCP = func(context.Context, sshclient.ConnConfig) (net.Conn, io.Closer, error) {
		return nil, nil, fmt.Errorf("connection refused")
	}

	var out bytes.Buffer
	cmd := newProbeCommand()
	cmd.SetContext(withWriter(withConfig(context.Background(), store), output.New(&out, &bytes.Buffer{}, output.WithJSON())))
	cmd.SetArgs([]string{"target"})
	err := cmd.Execute()
	assertBadNews(t, err)

	var envelope struct {
		Data probe.Result `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if envelope.Data.Reachable || envelope.Data.Alias != "target" {
		t.Fatalf("probe data = %+v", envelope.Data)
	}
}

func TestProbeAllUnreachableRendersDataAndReturnsBadNews(t *testing.T) {
	store := probeStoreForTest(t)
	originalDial := dialProbeTCP
	t.Cleanup(func() { dialProbeTCP = originalDial })
	dialProbeTCP = func(context.Context, sshclient.ConnConfig) (net.Conn, io.Closer, error) {
		return nil, nil, fmt.Errorf("connection refused")
	}

	var out bytes.Buffer
	w := output.New(&out, &bytes.Buffer{}, output.WithJSON())
	cmd := newProbeCommand()
	cmd.SetContext(context.Background())
	err := runProbeAll(cmd, store, w, time.Second, "", false)
	assertBadNews(t, err)

	var envelope struct {
		Data []probe.Result `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if len(envelope.Data) != 2 {
		t.Fatalf("probe all data = %+v", envelope.Data)
	}
	for _, result := range envelope.Data {
		if result.Reachable {
			t.Fatalf("probe all unexpectedly reachable: %+v", result)
		}
	}
}

func assertBadNews(t *testing.T, err error) {
	t.Helper()
	var badNews *output.BadNewsError
	if !errors.As(err, &badNews) || badNews.ProcessExitCode() != 1 {
		t.Fatalf("error = %v, want bad-news process exit 1", err)
	}
}

func TestProbeCommandHasDirectFlag(t *testing.T) {
	flag := newProbeCommand().Flags().Lookup("direct")
	if flag == nil || flag.DefValue != "false" {
		t.Fatalf("direct flag = %+v", flag)
	}
}

func TestProbeTargetForHostPaths(t *testing.T) {
	store := probeStoreForTest(t)
	host, err := store.Get("target")
	if err != nil {
		t.Fatal(err)
	}
	originalDial := dialProbeTCP
	t.Cleanup(func() { dialProbeTCP = originalDial })

	tests := []struct {
		name          string
		direct        bool
		portOverride  string
		wantPath      string
		wantPort      string
		wantProxyDial bool
	}{
		{name: "via proxy", wantPath: "via-proxy", wantPort: "2222", wantProxyDial: true},
		{name: "direct override", direct: true, portOverride: "2200", wantPath: "direct", wantPort: "2200"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, peer := net.Pipe()
			defer peer.Close()
			dialProbeTCP = func(_ context.Context, cfg sshclient.ConnConfig) (net.Conn, io.Closer, error) {
				if cfg.Port != tt.wantPort {
					t.Fatalf("dial port = %q, want %q", cfg.Port, tt.wantPort)
				}
				if tt.wantProxyDial {
					if cfg.ProxyJump != "jump" || cfg.ProxyConfig == nil || cfg.ProxyConfig.Host != "192.0.2.10" {
						t.Fatalf("proxy config = %+v", cfg)
					}
				} else if cfg.ProxyJump != "" || cfg.ProxyConfig != nil {
					t.Fatalf("direct probe retained proxy config: %+v", cfg)
				}
				return client, client, nil
			}

			target, err := probeTargetForHost(context.Background(), host, store, 3*time.Second, tt.portOverride, tt.direct)
			if err != nil {
				t.Fatalf("probeTargetForHost: %v", err)
			}
			if target.ProbePath != tt.wantPath || target.ProxyJump != "jump" || target.ResolvedHostname != "10.0.0.20" {
				t.Fatalf("target metadata = %+v", target)
			}
			result := checkProbeTarget(context.Background(), target)
			if !result.Reachable || result.ProbePath != tt.wantPath || result.Port != tt.wantPort {
				t.Fatalf("probe result = %+v", result)
			}
		})
	}
}

func TestProbeResultJSONFields(t *testing.T) {
	b, err := json.Marshal(probeView{Result: probe.Result{
		Alias: "target", Host: "10.0.0.20", Port: "22", ResolvedHostname: "10.0.0.20",
		ProxyJump: "jump", ProbePath: "via-proxy", Reachable: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"resolved_hostname", "proxy_jump", "probe_path"} {
		if _, ok := got[field]; !ok {
			t.Errorf("JSON missing %q: %s", field, b)
		}
	}
}
