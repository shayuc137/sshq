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

	"github.com/shayuc137/sshq/internal/audit"
	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/shayuc137/sshq/internal/sshclient"
	"github.com/spf13/cobra"
)

// clusterHostResult is one host's outcome in a cluster exec.
type clusterHostResult struct {
	Alias    string `json:"alias"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode *int   `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

// clusterResult aggregates all host outcomes plus a summary; it is Renderable.
type clusterResult struct {
	Results []clusterHostResult `json:"results"`
	Summary ipc.ClusterSummary  `json:"summary"`
}

func (cr clusterResult) Pretty() string {
	var b strings.Builder
	for _, r := range cr.Results {
		if r.Error != "" {
			fmt.Fprintf(&b, "[%s] error: %s\n", r.Alias, r.Error)
			continue
		}
		for _, line := range strings.Split(r.Stdout, "\n") {
			if line != "" {
				fmt.Fprintf(&b, "[%s] %s\n", r.Alias, line)
			}
		}
		for _, line := range strings.Split(r.Stderr, "\n") {
			if line != "" {
				fmt.Fprintf(&b, "[%s] stderr: %s\n", r.Alias, line)
			}
		}
		if r.ExitCode != nil && *r.ExitCode != 0 {
			fmt.Fprintf(&b, "[%s] exit=%d\n", r.Alias, *r.ExitCode)
		}
	}
	fmt.Fprintf(&b, "total=%d success=%d failed=%d", cr.Summary.Total, cr.Summary.Success, cr.Summary.Failed)
	return b.String()
}

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
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists").WithCode(output.CodeConfigUnavailable)
			}
			w := writerFrom(cmd.Context())

			tag, _ := cmd.Flags().GetString("tag")
			env, _ := cmd.Flags().GetString("env")
			hostsFlag, _ := cmd.Flags().GetString("hosts")
			all, _ := cmd.Flags().GetBool("all")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			timeout, _ := cmd.Flags().GetDuration("timeout")
			noDaemon, _ := cmd.Flags().GetBool("no-daemon")

			aliases, err := resolveClusterAliases(store, hostsFlag, tag, env, all)
			if err != nil {
				return err
			}

			command := args[0]

			if !noDaemon && ipc.IsRunning() {
				env, _ := ipc.MakeEnvelope("cluster-exec", ipc.ClusterExecPayload{
					Aliases:     aliases,
					Command:     command,
					Timeout:     int(timeout.Seconds()),
					Concurrency: concurrency,
					Verbose:     w.IsVerbose(),
				})
				return daemonDispatch(env,
					func(conn net.Conn) error {
						return recvClusterFrames(w, conn)
					},
					func(reason string) error {
						w.Info(reason + ", falling back to direct connection")
						return clusterExecDirectCLI(cmd, w, store, aliases, command, timeout, concurrency)
					},
				)
			}

			return clusterExecDirectCLI(cmd, w, store, aliases, command, timeout, concurrency)
		},
	}

	cmd.Flags().String("tag", "", "filter hosts by tag")
	cmd.Flags().String("env", "", "filter hosts by environment")
	cmd.Flags().String("hosts", "", "comma-separated host aliases")
	cmd.Flags().Bool("all", false, "target all configured hosts")
	cmd.Flags().Int("concurrency", 10, "max concurrent connections")
	cmd.Flags().Bool("no-daemon", false, "skip daemon, connect directly")

	return cmd
}

func resolveClusterAliases(store *config.Store, hostsFlag, tag, env string, all bool) ([]string, error) {
	if hostsFlag != "" {
		if all || tag != "" || env != "" {
			return nil, output.Errorf("--hosts cannot be combined with --tag, --env, or --all", "use exactly one host selector").WithCode(output.CodeInvalidUsage)
		}
		return aliasesFromHostsFlag(store, hostsFlag)
	}

	if !all && tag == "" && env == "" {
		return nil, output.Errorf("specify --hosts, --tag, --env, or --all", "usage: sshq cluster exec --all \"command\"").WithCode(output.CodeInvalidUsage)
	}

	hosts := store.Filter(config.Filter{Tag: tag, Env: env, All: all})
	if len(hosts) == 0 {
		return nil, output.Errorf("no hosts matched the filter", "check tags/env with 'sshq ls'").WithCode(output.CodeInvalidUsage)
	}

	aliases := make([]string, len(hosts))
	for i, h := range hosts {
		aliases[i] = h.Alias
	}
	return aliases, nil
}

func aliasesFromHostsFlag(store *config.Store, hostsFlag string) ([]string, error) {
	parts := strings.Split(hostsFlag, ",")
	aliases := make([]string, 0, len(parts))
	missing := make([]string, 0)
	seen := make(map[string]bool, len(parts))
	hasEmpty := false

	for _, part := range parts {
		alias := strings.TrimSpace(part)
		if alias == "" {
			hasEmpty = true
			continue
		}
		if seen[alias] {
			continue
		}
		seen[alias] = true

		if _, err := store.Get(alias); err != nil {
			missing = append(missing, alias)
			continue
		}
		aliases = append(aliases, alias)
	}

	if hasEmpty || len(aliases)+len(missing) == 0 {
		return nil, output.Errorf("invalid --hosts value", "use comma-separated aliases, for example --hosts rn,wee").WithCode(output.CodeInvalidUsage)
	}
	if len(missing) > 0 {
		return nil, output.Errorf("hosts not found: "+strings.Join(missing, ", "), "run 'sshq ls' to see available hosts").WithCode(output.CodeHostNotFound)
	}
	return aliases, nil
}

func recvClusterFrames(w *output.Writer, conn net.Conn) error {
	hostData := make(map[string]*clusterHostResult)
	var order []string
	hasError := false

	for {
		msg, err := ipc.Recv(conn)
		if err != nil {
			return output.Errorf("daemon connection lost", "").WithCode(output.CodeResultIndeterminate)
		}

		var frame ipc.Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			return output.Errorf("invalid daemon response", "").WithCode(output.CodeDaemonError)
		}

		switch frame.Type {
		case daemonVerboseFrame:
			recvVerboseFrame(w, frame)
		case "cluster":
			var cf ipc.ClusterFrame
			json.Unmarshal(frame.Payload, &cf)
			r, ok := hostData[cf.Alias]
			if !ok {
				r = &clusterHostResult{Alias: cf.Alias}
				hostData[cf.Alias] = r
				order = append(order, cf.Alias)
			}
			switch cf.Type {
			case "stdout":
				r.Stdout += cf.Data
			case "stderr":
				r.Stderr += cf.Data
			case "exit":
				exitCode := cf.Code
				r.ExitCode = &exitCode
			case "error":
				r.Error = cf.Hint
				hasError = true
			}

		case "result":
			var summary ipc.ClusterSummary
			json.Unmarshal(frame.Payload, &summary)

			results := make([]clusterHostResult, 0, len(order))
			for _, a := range order {
				hr := hostData[a]
				hr.Stdout = trimTrailingNewline(hr.Stdout)
				hr.Stderr = trimTrailingNewline(hr.Stderr)
				if hr.ExitCode != nil && *hr.ExitCode != 0 {
					hasError = true
				}
				results = append(results, *hr)
			}
			sort.Slice(results, func(i, j int) bool { return results[i].Alias < results[j].Alias })

			w.Render(clusterResult{Results: results, Summary: summary})
			if hasError {
				return output.BadNews()
			}
			return nil

		case "error":
			return output.Errorf(frame.Hint, frame.Action).WithCode(output.CodeOrInternal(frame.ErrorCode()))
		}
	}
}

func clusterExecDirectCLI(cmd *cobra.Command, w *output.Writer, store *config.Store, aliases []string, command string, timeout time.Duration, concurrency int) error {
	if err := checkPolicyClusterCommand(cmd.Context(), aliases, command); err != nil {
		return err
	}

	if concurrency <= 0 {
		concurrency = 10
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	auditStart := time.Now()
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var results []clusterHostResult

	for _, alias := range aliases {
		wg.Add(1)
		sem <- struct{}{}
		go func(alias string) {
			defer wg.Done()
			defer func() { <-sem }()

			host, err := store.Get(alias)
			if err != nil {
				mu.Lock()
				results = append(results, clusterHostResult{Alias: alias, Error: "host not found"})
				mu.Unlock()
				return
			}

			cfg, err := hostToConnConfigWithCredentials(host, store, credentialStoreFrom(cmd.Context()))
			if err != nil {
				mu.Lock()
				results = append(results, clusterHostResult{Alias: alias, Error: credentialErrorSummary(err)})
				mu.Unlock()
				return
			}
			cfg.Timeout = timeout

			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()

			client, err := sshclient.Dial(ctx, cfg)
			if err != nil {
				mu.Lock()
				results = append(results, clusterHostResult{Alias: alias, Error: err.Error()})
				mu.Unlock()
				return
			}
			defer client.Close()

			profile, _ := remote.GetProfile(ctx, client, profileCacheFrom(ctx), host.HostName, host.Port)
			shell := shellForExec(profile, "")
			result, err := exec.RunBufferedWithShell(ctx, client, command, shell)
			if err != nil {
				mu.Lock()
				results = append(results, clusterHostResult{Alias: alias, Error: err.Error()})
				mu.Unlock()
				return
			}
			normalizeRemoteResult(result, profile, shell)

			exitCode := result.ExitCode
			mu.Lock()
			results = append(results, clusterHostResult{
				Alias:    alias,
				Stdout:   trimTrailingNewline(result.Stdout),
				Stderr:   trimTrailingNewline(result.Stderr),
				ExitCode: &exitCode,
			})
			mu.Unlock()
		}(alias)
	}

	wg.Wait()

	sort.Slice(results, func(i, j int) bool { return results[i].Alias < results[j].Alias })

	success := 0
	hasError := false
	for _, r := range results {
		if r.Error != "" || r.ExitCode == nil || *r.ExitCode != 0 {
			hasError = true
		} else {
			success++
		}
	}

	auditResult := audit.ResultSuccess
	if hasError {
		auditResult = audit.ResultError
	}
	if err := recordAudit(cmd.Context(), audit.ClusterEntry(aliases, command, auditResult, time.Since(auditStart).Milliseconds(), audit.SourceDirect)); err != nil {
		return err
	}
	w.Render(clusterResult{
		Results: results,
		Summary: ipc.ClusterSummary{Total: len(results), Success: success, Failed: len(results) - success},
	})

	if hasError {
		return output.BadNews()
	}
	return nil
}
