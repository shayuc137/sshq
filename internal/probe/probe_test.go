package probe

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestCheck_Reachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	_, port, _ := net.SplitHostPort(ln.Addr().String())
	r := Check(context.Background(), "127.0.0.1", port, 2*time.Second)

	if !r.Reachable {
		t.Errorf("expected reachable, got error: %s", r.Error)
	}
	if r.LatencyMs < 0 {
		t.Errorf("latency should be >= 0, got %d", r.LatencyMs)
	}
}

func TestCheck_Unreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()

	r := Check(context.Background(), "127.0.0.1", port, 2*time.Second)
	if r.Reachable {
		t.Error("expected unreachable for closed port")
	}
	if r.Error == "" {
		t.Error("expected error message")
	}
}

func TestCheck_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := Check(ctx, "192.0.2.1", "22", 5*time.Second)
	if r.Reachable {
		t.Error("expected unreachable with cancelled context")
	}
}

func TestCheckAll(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	_, port, _ := net.SplitHostPort(ln.Addr().String())

	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, badPort, _ := net.SplitHostPort(ln2.Addr().String())
	ln2.Close()

	targets := []Target{
		{Alias: "local", Host: "127.0.0.1", Port: port},
		{Alias: "bad", Host: "127.0.0.1", Port: badPort},
	}

	results := CheckAll(context.Background(), targets, 500*time.Millisecond, 2)
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
