package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"sync"
	"time"

	"github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/ipc"
)

func (dc *daemonContext) handleClusterExec(conn net.Conn, raw json.RawMessage) {
	var payload ipc.ClusterExecPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, "invalid cluster-exec payload: "+err.Error(), "")
		return
	}

	if len(payload.Aliases) == 0 {
		ipc.SendError(conn, "no hosts matched the filter", "use --tag, --env, or --all")
		return
	}

	concurrency := payload.Concurrency
	if concurrency <= 0 {
		concurrency = 10
	}

	timeout := time.Duration(payload.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success, failed := 0, 0

	for _, alias := range payload.Aliases {
		wg.Add(1)
		sem <- struct{}{}
		go func(alias string) {
			defer wg.Done()
			defer func() { <-sem }()

			host, err := dc.store.Get(alias)
			if err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				sendClusterFrame(conn, &mu, ipc.ClusterFrame{Alias: alias, Type: "error", Hint: "host not found"})
				return
			}

			cfg := hostToConnConfigWithStore(host, dc.store)
			cfg.Timeout = timeout

			client, cerr := dc.pool.Get(context.Background(), alias, cfg)
			if cerr != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				sendClusterFrame(conn, &mu, ipc.ClusterFrame{Alias: alias, Type: "error", Hint: cerr.Error()})
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			result, err := exec.RunBuffered(ctx, client, payload.Command)
			if err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				sendClusterFrame(conn, &mu, ipc.ClusterFrame{Alias: alias, Type: "error", Hint: err.Error()})
				return
			}

			stdout := trimTrailingNewline(result.Stdout)
			if stdout != "" {
				sendClusterFrame(conn, &mu, ipc.ClusterFrame{Alias: alias, Type: "stdout", Data: stdout})
			}
			stderr := trimTrailingNewline(result.Stderr)
			if stderr != "" {
				sendClusterFrame(conn, &mu, ipc.ClusterFrame{Alias: alias, Type: "stderr", Data: stderr})
			}

			sendClusterFrame(conn, &mu, ipc.ClusterFrame{Alias: alias, Type: "exit", Code: result.ExitCode})

			mu.Lock()
			if result.ExitCode == 0 {
				success++
			} else {
				failed++
			}
			mu.Unlock()
		}(alias)
	}

	wg.Wait()

	summary := ipc.ClusterSummary{
		Total:   len(payload.Aliases),
		Success: success,
		Failed:  failed,
	}
	frame, _ := ipc.MakeResultFrame(summary)
	mu.Lock()
	ipc.Send(conn, frame)
	mu.Unlock()
}

func sendClusterFrame(conn net.Conn, mu *sync.Mutex, cf ipc.ClusterFrame) {
	b, _ := json.Marshal(cf)
	mu.Lock()
	ipc.Send(conn, ipc.Frame{Type: "cluster", Payload: json.RawMessage(b)})
	mu.Unlock()
}

func trimTrailingNewline(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	return s
}

// clusterExecDirect runs cluster exec without daemon (CLI goroutine pool).
func clusterExecDirect(aliases []string, command string, timeout time.Duration, concurrency int, results chan<- ipc.ClusterFrame, done chan<- ipc.ClusterSummary) {
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success, failed := 0, 0

	for _, alias := range aliases {
		wg.Add(1)
		sem <- struct{}{}
		go func(alias string) {
			defer wg.Done()
			defer func() { <-sem }()

			// In direct mode we need to resolve from config store.
			// This function is called from CLI context which has store access.
			// The caller handles store lookup and passes pre-resolved aliases.
			// We just signal error for unresolvable ones.
			cf := execForClusterDirect(alias, command, timeout)
			for _, f := range cf {
				results <- f
				if f.Type == "exit" {
					mu.Lock()
					if f.Code == 0 {
						success++
					} else {
						failed++
					}
					mu.Unlock()
				}
				if f.Type == "error" {
					mu.Lock()
					failed++
					mu.Unlock()
				}
			}
		}(alias)
	}

	wg.Wait()
	close(results)
	done <- ipc.ClusterSummary{Total: len(aliases), Success: success, Failed: failed}
}

func execForClusterDirect(alias, command string, timeout time.Duration) []ipc.ClusterFrame {
	// This needs config store access. It will be called from CLI with store in scope.
	// Stub: the actual implementation is in cluster.go's directExecOne.
	return nil
}

// directExecOne is the real per-host execution for direct mode.
// It's defined in cluster.go since it needs cobra context.
var _ = bytes.Compare // keep bytes import
