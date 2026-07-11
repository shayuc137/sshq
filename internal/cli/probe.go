package cli

import (
	"context"
	"encoding/json"
	"io"
	"net"
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
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists").WithCode(output.CodeConfigUnavailable)
			}

			w := writerFrom(cmd.Context())
			timeout, _ := cmd.Flags().GetDuration("timeout")
			portOverride, _ := cmd.Flags().GetString("port")
			all, _ := cmd.Flags().GetBool("all")
			direct, _ := cmd.Flags().GetBool("direct")

			refreshProfile, _ := cmd.Flags().GetBool("refresh-profile")

			if all {
				return runProbeAll(cmd, store, w, timeout, portOverride, direct)
			}

			if len(args) == 0 {
				return output.Errorf("alias required", "use 'sshq probe <alias>' or 'sshq probe --all'").WithCode(output.CodeInvalidUsage)
			}

			alias := args[0]
			host, err := store.Get(alias)
			if err != nil {
				return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts").WithCode(output.CodeHostNotFound)
			}

			target, err := probeTargetForHost(cmd.Context(), host, store, timeout, portOverride, direct)
			if err != nil {
				return output.Errorf(credentialErrorSummary(err), "").WithCode(output.CodeCredentialError)
			}

			r := checkProbeTarget(cmd.Context(), target)
			w.Verbose(fmtProbeConnection(r))

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
	cmd.Flags().Bool("direct", false, "skip ProxyJump and probe the target directly")
	cmd.Flags().Bool("refresh-profile", false, "detect and cache remote OS/shell profile")

	return cmd
}

func refreshRemoteProfile(cmd *cobra.Command, w *output.Writer, host config.Host) *remote.Profile {
	if ipc.IsRunning() {
		return refreshProfileThroughDaemon(w, host)
	}
	return refreshProfileDirect(cmd, w, host)
}

func refreshProfileThroughDaemon(w *output.Writer, host config.Host) *remote.Profile {
	conn, err := ipc.Connect()
	if err != nil {
		w.Info("daemon unreachable for profile detect")
		return nil
	}
	defer conn.Close()

	env, _ := ipc.MakeEnvelope("profile", ipc.ProfilePayload{
		Alias:   host.Alias,
		Refresh: true,
		Verbose: w.IsVerbose(),
	})
	if err := ipc.Send(conn, env); err != nil {
		w.Info("daemon send failed for profile detect")
		return nil
	}

	for {
		msg, err := ipc.Recv(conn)
		if err != nil {
			w.Info("daemon recv failed for profile detect")
			return nil
		}

		var frame ipc.Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			return nil
		}

		switch frame.Type {
		case daemonVerboseFrame:
			recvVerboseFrame(w, frame)
		case "error":
			w.Info("profile detect: " + frame.Hint)
			return nil
		case "result":
			var pr ipc.ProfileResult
			json.Unmarshal(frame.Payload, &pr)
			return &remote.Profile{
				OS:       remote.OS(pr.OS),
				Shell:    remote.Shell(pr.Shell),
				Encoding: pr.Encoding,
				HomeDir:  pr.HomeDir,
			}
		}
	}
}

func refreshProfileDirect(cmd *cobra.Command, w *output.Writer, host config.Host) *remote.Profile {
	timeout, _ := cmd.Flags().GetDuration("timeout")
	store := configFrom(cmd.Context())
	cfg, err := hostToConnConfigWithCredentials(host, store, credentialStoreFrom(cmd.Context()))
	if err != nil {
		w.Info("profile detect: " + credentialErrorSummary(err))
		return nil
	}
	cfg.Timeout = timeout
	ctx := cmd.Context()
	start := time.Now()
	client, err := sshclient.Dial(ctx, cfg)
	if err != nil {
		w.Info("profile detect: SSH connect failed")
		return nil
	}
	defer client.Close()
	w.Verbose("connection: alias=" + host.Alias + " duration=" + verboseDuration(time.Since(start)) + " direct")

	cache := profileCacheFrom(ctx)
	if cache != nil {
		cache.Invalidate(host.HostName, host.Port)
	}
	p, err := remote.GetProfile(ctx, client, cache, host.HostName, host.Port)
	if err != nil {
		w.Info("profile detect: " + err.Error())
	}
	w.Verbose(verboseProfile(p))
	return p
}

func runProbeAll(cmd *cobra.Command, store *config.Store, w *output.Writer, timeout time.Duration, portOverride string, direct bool) error {
	hosts := store.List()
	targets := make([]probe.Target, len(hosts))
	for i, h := range hosts {
		target, err := probeTargetForHost(cmd.Context(), h, store, timeout, portOverride, direct)
		if err != nil {
			port := h.Port
			if portOverride != "" {
				port = portOverride
			}
			target = probe.Target{
				Alias: h.Alias, Host: h.HostName, Port: port, ResolvedHostname: h.HostName,
				ProxyJump: h.ProxyJump, ProbePath: probePath(h.ProxyJump, direct),
				Dialer: failedProbeDialer(err),
			}
		}
		targets[i] = target
	}

	results := probe.CheckAll(cmd.Context(), targets, 10)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Alias < results[j].Alias
	})
	for _, result := range results {
		w.Verbose(fmtProbeConnection(result))
	}
	w.Render(probeList(results))
	return nil
}

var dialProbeTCP = sshclient.DialTCP

func probeTargetForHost(ctx context.Context, host config.Host, store *config.Store, timeout time.Duration, portOverride string, direct bool) (probe.Target, error) {
	cfg, err := hostToConnConfigWithCredentials(host, store, credentialStoreFrom(ctx))
	if err != nil {
		return probe.Target{}, err
	}
	if portOverride != "" {
		cfg.Port = portOverride
	}
	cfg.Timeout = timeout
	if direct {
		cfg.ProxyJump = ""
		cfg.ProxyConfig = nil
	}

	return probe.Target{
		Alias:            host.Alias,
		Host:             cfg.Host,
		Port:             cfg.Port,
		ResolvedHostname: cfg.Host,
		ProxyJump:        host.ProxyJump,
		ProbePath:        probePath(host.ProxyJump, direct),
		Dialer: func(ctx context.Context) (net.Conn, io.Closer, error) {
			return dialProbeTCP(ctx, cfg)
		},
	}, nil
}

func probePath(proxyJump string, direct bool) string {
	if !direct && proxyJump != "" {
		return "via-proxy"
	}
	return "direct"
}

func checkProbeTarget(ctx context.Context, target probe.Target) probe.Result {
	r := probe.Check(ctx, target.Dialer)
	r.Alias = target.Alias
	r.Host = target.Host
	r.Port = target.Port
	r.ResolvedHostname = target.ResolvedHostname
	r.ProxyJump = target.ProxyJump
	r.ProbePath = target.ProbePath
	return r
}

func failedProbeDialer(err error) probe.Dialer {
	return func(context.Context) (net.Conn, io.Closer, error) {
		return nil, nil, err
	}
}

type probeView struct {
	probe.Result
	Profile *remote.Profile `json:"profile,omitempty"`
}

func (v probeView) Pretty() string {
	s := probe.RenderCompact(v.Result)
	if suffix := remote.RenderProfileCompact(v.Profile); suffix != "" {
		s += " " + suffix
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

func fmtProbeConnection(r probe.Result) string {
	if r.Reachable {
		return "connection: alias=" + r.Alias + " tcp_probe=" + verboseDuration(time.Duration(r.LatencyMs)*time.Millisecond)
	}
	return "connection: alias=" + r.Alias + " tcp_probe=failed reason=" + r.Error
}
