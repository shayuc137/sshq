package probe

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

type Result struct {
	Alias     string `json:"alias"`
	Host      string `json:"host"`
	Port      string `json:"port"`
	Reachable bool   `json:"reachable"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

func Check(ctx context.Context, host, port string, timeout time.Duration) Result {
	addr := net.JoinHostPort(host, port)
	start := time.Now()

	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	elapsed := time.Since(start)

	if err != nil {
		errMsg := "connection failed"
		if ctx.Err() != nil {
			errMsg = "timeout"
		} else if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
			errMsg = "timeout"
		}
		return Result{
			Host:  host,
			Port:  port,
			Error: errMsg,
		}
	}
	conn.Close()

	return Result{
		Host:      host,
		Port:      port,
		Reachable: true,
		LatencyMs: elapsed.Milliseconds(),
	}
}

func CheckAll(ctx context.Context, targets []Target, timeout time.Duration, concurrency int) []Result {
	if concurrency <= 0 {
		concurrency = 10
	}

	results := make([]Result, len(targets))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, t := range targets {
		wg.Add(1)
		go func(idx int, target Target) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r := Check(ctx, target.Host, target.Port, timeout)
			r.Alias = target.Alias
			results[idx] = r
		}(i, t)
	}

	wg.Wait()
	return results
}

type Target struct {
	Alias string
	Host  string
	Port  string
}

func RenderCompact(r Result) string {
	if r.Reachable {
		return fmt.Sprintf("%s %s:%s ok %dms", r.Alias, r.Host, r.Port, r.LatencyMs)
	}
	return fmt.Sprintf("%s %s:%s fail %s", r.Alias, r.Host, r.Port, r.Error)
}

func RenderBatchSummary(results []Result) string {
	ok, fail := 0, 0
	for _, r := range results {
		if r.Reachable {
			ok++
		} else {
			fail++
		}
	}
	return fmt.Sprintf("total=%d ok=%d fail=%d", len(results), ok, fail)
}

func RenderPretty(r Result) string {
	if r.Reachable {
		return fmt.Sprintf("%-15s %s:%s  ✓ reachable (%dms)", r.Alias, r.Host, r.Port, r.LatencyMs)
	}
	return fmt.Sprintf("%-15s %s:%s  ✗ %s", r.Alias, r.Host, r.Port, r.Error)
}
