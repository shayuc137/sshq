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

			refreshProfile, _ := cmd.Flags().GetBool("refresh-profile")

			if all {
				return runProbeAll(cmd, store, w, timeout, portOverride)
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

			w.Render(probeView{Result: r, Profile: profile})
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
	store := configFrom(cmd.Context())
	cfg := hostToConnConfigWithStore(host, store)
	cfg.Timeout = timeout
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

func runProbeAll(cmd *cobra.Command, store *config.Store, w *output.Writer, timeout time.Duration, portOverride string) error {
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
	sort.Slice(results, func(i, j int) bool {
		return results[i].Alias < results[j].Alias
	})
	w.Render(probeList(results))
	return nil
}

type probeView struct {
	probe.Result
	Profile *remote.Profile `json:"profile,omitempty"`
}

func (v probeView) Pretty() string {
	s := probe.RenderCompact(v.Result)
	if v.Profile != nil {
		s += fmt.Sprintf(" os=%s shell=%s", v.Profile.OS, v.Profile.Shell)
		if v.Profile.Encoding != "" {
			s += " encoding=" + v.Profile.Encoding
		}
	}
	return s
}

type probeList []probe.Result

func (pl probeList) Pretty() string {
	var b strings.Builder
	for _, r := range pl {
		b.WriteString(probe.RenderCompact(r) + "\n")
	}
	b.WriteString(probe.RenderBatchSummary(pl))
	return b.String()
}
