package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type Result struct {
	Alias            string `json:"alias"`
	Host             string `json:"host"`
	Port             string `json:"port"`
	ResolvedHostname string `json:"resolved_hostname"`
	ProxyJump        string `json:"proxy_jump"`
	ProbePath        string `json:"probe_path"`
	Reachable        bool   `json:"reachable"`
	LatencyMs        int64  `json:"latency_ms,omitempty"`
	Error            string `json:"error,omitempty"`
}

// Dialer establishes the TCP path to a target; latency is measured around it.
type Dialer func(context.Context) (net.Conn, io.Closer, error)

func Check(ctx context.Context, dial Dialer) Result {
	start := time.Now()
	_, closer, err := dial(ctx)
	elapsed := time.Since(start)

	if err != nil {
		errMsg := "connection failed"
		if ctx.Err() != nil {
			errMsg = "timeout"
		} else {
			var opErr *net.OpError
			if errors.As(err, &opErr) && opErr.Timeout() {
				errMsg = "timeout"
			}
		}
		return Result{Error: errMsg}
	}
	if closer == nil {
		return Result{Error: "connection failed"}
	}
	closer.Close()

	return Result{
		Reachable: true,
		LatencyMs: elapsed.Milliseconds(),
	}
}

func CheckAll(ctx context.Context, targets []Target, concurrency int) []Result {
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

			r := Check(ctx, target.Dialer)
			r.Alias = target.Alias
			r.Host = target.Host
			r.Port = target.Port
			r.ResolvedHostname = target.ResolvedHostname
			r.ProxyJump = target.ProxyJump
			r.ProbePath = target.ProbePath
			results[idx] = r
		}(i, t)
	}

	wg.Wait()
	return results
}

type Target struct {
	Alias            string
	Host             string
	Port             string
	ResolvedHostname string
	ProxyJump        string
	ProbePath        string
	Dialer           Dialer
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
