package cli

import (
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

