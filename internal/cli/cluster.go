package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/sshclient"
	"github.com/spf13/cobra"
)

func newClusterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Concurrent operations across multiple hosts",
	}
	cmd.AddCommand(newClusterExecCommand())
	return cmd
}

func newClusterExecCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <command>",
		Short: "Execute a command on multiple hosts concurrently",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists")
			}
			w := writerFrom(cmd.Context())

			tag, _ := cmd.Flags().GetString("tag")
			env, _ := cmd.Flags().GetString("env")
			all, _ := cmd.Flags().GetBool("all")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			timeout, _ := cmd.Flags().GetDuration("timeout")
			noDaemon, _ := cmd.Flags().GetBool("no-daemon")

			if !all && tag == "" && env == "" {
				return output.Errorf("specify --tag, --env, or --all", "usage: sshq cluster exec --all \"command\"")
			}

			hosts := store.Filter(config.Filter{Tag: tag, Env: env, All: all})
			if len(hosts) == 0 {
				return output.Errorf("no hosts matched the filter", "check tags/env with 'sshq ls'")
			}

			aliases := make([]string, len(hosts))
			for i, h := range hosts {
				aliases[i] = h.Alias
			}

			command := args[0]

			if !noDaemon && ipc.IsRunning() {
				return clusterExecViaDaemon(w, aliases, command, timeout, concurrency)
			}

			return clusterExecDirectCLI(cmd, w, store, aliases, command, timeout, concurrency)
		},
	}

	cmd.Flags().String("tag", "", "filter hosts by tag")
	cmd.Flags().String("env", "", "filter hosts by environment")
	cmd.Flags().Bool("all", false, "target all configured hosts")
	cmd.Flags().Int("concurrency", 10, "max concurrent connections")
	cmd.Flags().Bool("no-daemon", false, "skip daemon, connect directly")

	return cmd
}

func clusterExecViaDaemon(w *output.Writer, aliases []string, command string, timeout time.Duration, concurrency int) error {
	conn, err := ipc.Connect()
	if err != nil {
		w.Info("daemon unreachable, falling back to direct connection")
		return nil
	}
	defer conn.Close()

	env, _ := ipc.MakeEnvelope("cluster-exec", ipc.ClusterExecPayload{
		Aliases:     aliases,
		Command:     command,
		Timeout:     int(timeout.Seconds()),
		Concurrency: concurrency,
	})
	if err := ipc.Send(conn, env); err != nil {
		return output.Errorf("daemon send failed", "")
	}

	return recvClusterFrames(w, conn)
}

func recvClusterFrames(w *output.Writer, conn net.Conn) error {
	type jsonResult struct {
		Alias    string `json:"alias"`
		Stdout   string `json:"stdout,omitempty"`
		Stderr   string `json:"stderr,omitempty"`
		ExitCode int    `json:"exit_code"`
		Error    string `json:"error,omitempty"`
	}

	var jsonResults []jsonResult
	hostData := make(map[string]*jsonResult)
	hasError := false

	for {
		msg, err := ipc.Recv(conn)
		if err != nil {
			return output.Errorf("daemon connection lost", "")
		}

		var frame ipc.Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			return output.Errorf("invalid daemon response", "")
		}

		switch frame.Type {
		case "cluster":
			var cf ipc.ClusterFrame
			json.Unmarshal(frame.Payload, &cf)

			if w.IsJSONMode() {
				if _, ok := hostData[cf.Alias]; !ok {
					r := &jsonResult{Alias: cf.Alias}
					hostData[cf.Alias] = r
					jsonResults = append(jsonResults, *r)
				}
				r := hostData[cf.Alias]
				switch cf.Type {
				case "stdout":
					r.Stdout = cf.Data
				case "stderr":
					r.Stderr = cf.Data
				case "exit":
					r.ExitCode = cf.Code
				case "error":
					r.Error = cf.Hint
					hasError = true
				}
			} else {
				switch cf.Type {
				case "stdout":
					for _, line := range strings.Split(cf.Data, "\n") {
						if line != "" {
							w.Value(fmt.Sprintf("[%s] %s", cf.Alias, line))
						}
					}
				case "stderr":
					w.Info(fmt.Sprintf("[%s] %s", cf.Alias, cf.Data))
				case "exit":
					if cf.Code != 0 {
						w.Info(fmt.Sprintf("[%s] exit=%d", cf.Alias, cf.Code))
						hasError = true
					}
				case "error":
					w.Info(fmt.Sprintf("[%s] error: %s", cf.Alias, cf.Hint))
					hasError = true
				}
			}

		case "result":
			var summary ipc.ClusterSummary
			json.Unmarshal(frame.Payload, &summary)

			if w.IsJSONMode() {
				finalResults := make([]jsonResult, 0, len(jsonResults))
				for _, r := range jsonResults {
					if updated, ok := hostData[r.Alias]; ok {
						finalResults = append(finalResults, *updated)
					}
				}
				w.JSONOut(map[string]any{
					"results": finalResults,
					"summary": summary,
				})
			} else {
				w.Value(fmt.Sprintf("total=%d success=%d failed=%d", summary.Total, summary.Success, summary.Failed))
			}

			if hasError {
				return &exec.ExitError{Code: 1}
			}
			return nil

		case "error":
			return output.Errorf(frame.Hint, frame.Action)
		}
	}
}

func clusterExecDirectCLI(cmd *cobra.Command, w *output.Writer, store *config.Store, aliases []string, command string, timeout time.Duration, concurrency int) error {
	if concurrency <= 0 {
		concurrency = 10
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	type hostResult struct {
		Alias    string
		Stdout   string
		Stderr   string
		ExitCode int
		Error    string
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var results []hostResult

	for _, alias := range aliases {
		wg.Add(1)
		sem <- struct{}{}
		go func(alias string) {
			defer wg.Done()
			defer func() { <-sem }()

			host, err := store.Get(alias)
			if err != nil {
				mu.Lock()
				results = append(results, hostResult{Alias: alias, Error: "host not found"})
				mu.Unlock()
				return
			}

			cfg := hostToConnConfigWithStore(host, store)
			cfg.Timeout = timeout

			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()

			client, err := sshclient.Dial(ctx, cfg)
			if err != nil {
				mu.Lock()
				results = append(results, hostResult{Alias: alias, Error: err.Error()})
				mu.Unlock()
				return
			}
			defer client.Close()

			result, err := exec.RunBuffered(ctx, client, command)
			if err != nil {
				mu.Lock()
				results = append(results, hostResult{Alias: alias, Error: err.Error()})
				mu.Unlock()
				return
			}

			mu.Lock()
			results = append(results, hostResult{
				Alias:    alias,
				Stdout:   trimTrailingNewline(result.Stdout),
				Stderr:   trimTrailingNewline(result.Stderr),
				ExitCode: result.ExitCode,
			})
			mu.Unlock()
		}(alias)
	}

	wg.Wait()

	sort.Slice(results, func(i, j int) bool { return results[i].Alias < results[j].Alias })

	hasError := false
	success := 0

	if w.IsJSONMode() {
		for i := range results {
			if results[i].Error != "" || results[i].ExitCode != 0 {
				hasError = true
			} else {
				success++
			}
		}
		w.JSONOut(map[string]any{
			"results": results,
			"summary": ipc.ClusterSummary{Total: len(results), Success: success, Failed: len(results) - success},
		})
	} else {
		for _, r := range results {
			if r.Error != "" {
				w.Info(fmt.Sprintf("[%s] error: %s", r.Alias, r.Error))
				hasError = true
				continue
			}
			for _, line := range strings.Split(r.Stdout, "\n") {
				if line != "" {
					w.Value(fmt.Sprintf("[%s] %s", r.Alias, line))
				}
			}
			if r.ExitCode != 0 {
				w.Info(fmt.Sprintf("[%s] exit=%d", r.Alias, r.ExitCode))
				hasError = true
			} else {
				success++
			}
		}
		w.Value(fmt.Sprintf("total=%d success=%d failed=%d", len(results), success, len(results)-success))
	}

	if hasError {
		return &exec.ExitError{Code: 1}
	}
	return nil
}
