package probe

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestCheck_Reachable(t *testing.T) {
	client, peer := net.Pipe()
	t.Cleanup(func() { peer.Close() })
	peerClosed := make(chan struct{})
	go func() {
		io.Copy(io.Discard, peer)
		close(peerClosed)
	}()
	r := Check(context.Background(), func(context.Context) (net.Conn, io.Closer, error) {
		return client, client, nil
	})

	if !r.Reachable {
		t.Errorf("expected reachable, got error: %s", r.Error)
	}
	if r.LatencyMs < 0 {
		t.Errorf("latency should be >= 0, got %d", r.LatencyMs)
	}
	select {
	case <-peerClosed:
	case <-time.After(time.Second):
		t.Fatal("Check did not close the injected connection")
	}
}

func TestCheck_Unreachable(t *testing.T) {
	r := Check(context.Background(), func(context.Context) (net.Conn, io.Closer, error) {
		return nil, nil, errors.New("connection refused")
	})
	if r.Reachable {
		t.Error("expected unreachable")
	}
	if r.Error == "" {
		t.Error("expected error message")
	}
}

func TestCheck_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := Check(ctx, func(ctx context.Context) (net.Conn, io.Closer, error) {
		return nil, nil, ctx.Err()
	})
	if r.Reachable {
		t.Error("expected unreachable with cancelled context")
	}
	if r.Error != "timeout" {
		t.Fatalf("error = %q, want timeout", r.Error)
	}
}

func TestCheckAll(t *testing.T) {
	client, peer := net.Pipe()
	t.Cleanup(func() { peer.Close() })
	targets := []Target{
		{
			Alias: "local", Host: "127.0.0.1", Port: "22", ResolvedHostname: "127.0.0.1", ProbePath: "direct",
			Dialer: func(context.Context) (net.Conn, io.Closer, error) { return client, client, nil },
		},
		{
			Alias: "bad", Host: "10.0.0.20", Port: "2222", ResolvedHostname: "10.0.0.20",
			ProxyJump: "jump", ProbePath: "via-proxy",
			Dialer: func(context.Context) (net.Conn, io.Closer, error) {
				return nil, nil, errors.New("proxy failed")
			},
		},
	}

	results := CheckAll(context.Background(), targets, 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if !results[0].Reachable {
		t.Error("local should be reachable")
	}
	if results[0].Alias != "local" {
		t.Errorf("alias = %q, want %q", results[0].Alias, "local")
	}
	if results[1].Reachable {
		t.Error("bad should be unreachable")
	}
	if results[0].ProbePath != "direct" || results[1].ProbePath != "via-proxy" {
		t.Fatalf("probe paths = %q, %q", results[0].ProbePath, results[1].ProbePath)
	}
	if results[1].ProxyJump != "jump" || results[1].ResolvedHostname != "10.0.0.20" {
		t.Fatalf("proxy metadata = %+v", results[1])
	}
}

func TestCheckMeasuresDialerLatency(t *testing.T) {
	client, peer := net.Pipe()
	t.Cleanup(func() { peer.Close() })
	r := Check(context.Background(), func(context.Context) (net.Conn, io.Closer, error) {
		time.Sleep(5 * time.Millisecond)
		return client, client, nil
	})
	if r.LatencyMs < 4 {
		t.Fatalf("latency_ms = %d, want injected delay", r.LatencyMs)
	}
}

func TestRenderCompact(t *testing.T) {
	ok := Result{Alias: "ali", Host: "1.2.3.4", Port: "22", Reachable: true, LatencyMs: 42}
	got := RenderCompact(ok)
	want := "ali 1.2.3.4:22 ok 42ms"
	if got != want {
		t.Errorf("RenderCompact(ok) = %q, want %q", got, want)
	}

	fail := Result{Alias: "bad", Host: "1.2.3.4", Port: "22", Error: "timeout"}
	got = RenderCompact(fail)
	want = "bad 1.2.3.4:22 fail timeout"
	if got != want {
		t.Errorf("RenderCompact(fail) = %q, want %q", got, want)
	}
}

func TestRenderBatchSummary(t *testing.T) {
	results := []Result{
		{Reachable: true},
		{Reachable: false},
		{Reachable: true},
	}
	got := RenderBatchSummary(results)
	want := "total=3 ok=2 fail=1"
	if got != want {
		t.Errorf("RenderBatchSummary = %q, want %q", got, want)
	}
}
