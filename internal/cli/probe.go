package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/probe"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/shayuc137/sshq/internal/sshclient"
	"github.com/spf13/cobra"
)

func newProbeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "probe <alias>",
		Short: "Check TCP connectivity to a host",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists")
			}

			w := writerFrom(cmd.Context())
			timeout, _ := cmd.Flags().GetDuration("timeout")
			portOverride, _ := cmd.Flags().GetString("port")
			all, _ := cmd.Flags().GetBool("all")
			pretty, _ := cmd.Flags().GetBool("pretty")

			refreshProfile, _ := cmd.Flags().GetBool("refresh-profile")

			if all {
				return runProbeAll(cmd, store, w, timeout, portOverride, pretty)
			}

			if len(args) == 0 {
				return output.Errorf("alias required", "use 'sshq probe <alias>' or 'sshq probe --all'")
			}

			alias := args[0]
			host, err := store.Get(alias)
			if err != nil {
				return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts")
			}

			port := host.Port
			if portOverride != "" {
				port = portOverride
			}

			r := probe.Check(cmd.Context(), host.HostName, port, timeout)
			r.Alias = alias

			var profile *remote.Profile
			if refreshProfile && r.Reachable {
				profile = refreshRemoteProfile(cmd, w, host)
			}

			if w.IsJSONMode() {
				out := map[string]interface{}{
					"alias": r.Alias, "host": r.Host, "port": r.Port,
					"reachable": r.Reachable, "latency_ms": r.LatencyMs,
				}
				if r.Error != "" {
					out["error"] = r.Error
				}
				if profile != nil {
					out["profile"] = profile
				}
				w.JSONOut(out)
				return nil
			}

			var profileSuffix string
			if profile != nil {
				profileSuffix = fmt.Sprintf(" os=%s shell=%s", profile.OS, profile.Shell)
				if profile.Encoding != "" {
					profileSuffix += " encoding=" + profile.Encoding
				}
			}
			if pretty {
				w.Value(probe.RenderPretty(r) + profileSuffix)
			} else {
				w.Value(probe.RenderCompact(r) + profileSuffix)
			}
			return nil
		},
	}

	cmd.Flags().String("port", "", "override port to probe")
	cmd.Flags().Bool("all", false, "probe all configured hosts")
	cmd.Flags().Bool("refresh-profile", false, "detect and cache remote OS/shell profile")

	return cmd
}

func refreshRemoteProfile(cmd *cobra.Command, w *output.Writer, host config.Host) *remote.Profile {
	if ipc.IsRunning() {
		return refreshProfileViaDaemon(w, host)
	}
	return refreshProfileDirect(cmd, w, host)
}

func refreshProfileViaDaemon(w *output.Writer, host config.Host) *remote.Profile {
	conn, err := ipc.Connect()
	if err != nil {
		w.Info("daemon unreachable for profile detect")
		return nil
	}
	defer conn.Close()

	env, _ := ipc.MakeEnvelope("profile", ipc.ProfilePayload{
		Alias:   host.Alias,
		Refresh: true,
	})
	if err := ipc.Send(conn, env); err != nil {
		w.Info("daemon send failed for profile detect")
		return nil
	}

	msg, err := ipc.Recv(conn)
	if err != nil {
		w.Info("daemon recv failed for profile detect")
		return nil
	}

	var frame ipc.Frame
	if err := json.Unmarshal(msg, &frame); err != nil {
		return nil
	}

	if frame.Type == "error" {
		w.Info("profile detect: " + frame.Hint)
		return nil
	}

	if frame.Type == "result" {
		var pr ipc.ProfileResult
		json.Unmarshal(frame.Payload, &pr)
		return &remote.Profile{
			OS:       remote.OS(pr.OS),
			Shell:    remote.Shell(pr.Shell),
			Encoding: pr.Encoding,
			HomeDir:  pr.HomeDir,
		}
	}

	return nil
}

func refreshProfileDirect(cmd *cobra.Command, w *output.Writer, host config.Host) *remote.Profile {
	timeout, _ := cmd.Flags().GetDuration("timeout")
	cfg := sshclient.ConnConfig{
		Host: host.HostName, Port: host.Port,
		User: host.User, IdentityFile: host.IdentityFile,
		Timeout: timeout,
	}
	ctx := cmd.Context()
	client, err := sshclient.Dial(ctx, cfg)
	if err != nil {
		w.Info("profile detect: SSH connect failed")
		return nil
	}
	defer client.Close()

	cache := profileCacheFrom(ctx)
	if cache != nil {
		cache.Invalidate(host.HostName, host.Port)
	}
	p, err := remote.GetProfile(ctx, client, cache, host.HostName, host.Port)
	if err != nil {
		w.Info("profile detect: " + err.Error())
	}
	return p
}

func runProbeAll(cmd *cobra.Command, store *config.Store, w *output.Writer, timeout time.Duration, portOverride string, pretty bool) error {
	hosts := store.List()
	targets := make([]probe.Target, len(hosts))
	for i, h := range hosts {
		port := h.Port
		if portOverride != "" {
			port = portOverride
		}
		targets[i] = probe.Target{Alias: h.Alias, Host: h.HostName, Port: port}
	}

	results := probe.CheckAll(cmd.Context(), targets, timeout, 10)

	if w.IsJSONMode() {
		w.JSONOut(results)
		return nil
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Alias < results[j].Alias
	})

	var b strings.Builder
	for _, r := range results {
		if pretty {
			b.WriteString(probe.RenderPretty(r) + "\n")
		} else {
			b.WriteString(probe.RenderCompact(r) + "\n")
		}
	}
	b.WriteString(probe.RenderBatchSummary(results) + "\n")
	w.Value(b.String())
	return nil
}
