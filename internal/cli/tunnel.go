package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/sshclient"
	"github.com/shayuc137/sshq/internal/tunnel"
	"github.com/spf13/cobra"
)

func newTunnelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tunnel",
		Short: "Manage SSH port forwarding",
	}
	cmd.AddCommand(
		newTunnelStartCommand(),
		newTunnelListCommand(),
		newTunnelStopCommand(),
	)
	return cmd
}

func newTunnelStartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start <alias>",
		Short: "Start a port forwarding tunnel",
		Long: `Start an SSH tunnel for port forwarding.

Examples:
  sshq tunnel start ali -L 8080:localhost:80     local forward
  sshq tunnel start ali -R 9090:localhost:3000    remote forward`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists")
			}
			w := writerFrom(cmd.Context())
			alias := args[0]

			localFwd, _ := cmd.Flags().GetString("L")
			remoteFwd, _ := cmd.Flags().GetString("R")

			if localFwd == "" && remoteFwd == "" {
				return output.Errorf("specify -L or -R", "usage: sshq tunnel start <alias> -L <local:remote> or -R <remote:local>")
			}
			if localFwd != "" && remoteFwd != "" {
				return output.Errorf("specify either -L or -R, not both", "")
			}

			var direction, localAddr, remoteAddr string
			if localFwd != "" {
				direction = "local"
				la, ra, err := parseFwdSpec(localFwd)
				if err != nil {
					return output.Errorf(err.Error(), "format: <local_port>:<remote_host>:<remote_port>")
				}
				localAddr, remoteAddr = la, ra
			} else {
				direction = "remote"
				ra, la, err := parseFwdSpec(remoteFwd)
				if err != nil {
					return output.Errorf(err.Error(), "format: <remote_port>:<local_host>:<local_port>")
				}
				localAddr, remoteAddr = la, ra
			}

			if ipc.IsRunning() {
				env, _ := ipc.MakeEnvelope("tunnel-start", ipc.TunnelStartPayload{
					Direction:  direction,
					Alias:      alias,
					LocalAddr:  localAddr,
					RemoteAddr: remoteAddr,
					Verbose:    w.IsVerbose(),
				})
				return daemonDispatch(env,
					func(conn net.Conn) error {
						return recvTunnelStartResult(w, conn, alias, direction)
					},
					func(reason string) error {
						w.Info(reason + ", starting tunnel in foreground")
						return tunnelStartForeground(cmd, w, alias, direction, localAddr, remoteAddr)
					},
				)
			}

			return tunnelStartForeground(cmd, w, alias, direction, localAddr, remoteAddr)
		},
	}

	cmd.Flags().StringP("L", "L", "", "local forward: <local_port>:<remote_host>:<remote_port>")
	cmd.Flags().StringP("R", "R", "", "remote forward: <remote_port>:<local_host>:<local_port>")
	return cmd
}

func parseFwdSpec(spec string) (string, string, error) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) == 3 {
		return "localhost:" + parts[0], parts[1] + ":" + parts[2], nil
	}
	if len(parts) == 2 {
		return "localhost:" + parts[0], "localhost:" + parts[1], nil
	}
	return "", "", fmt.Errorf("invalid forward spec %q", spec)
}

func recvTunnelStartResult(w *output.Writer, conn net.Conn, alias, direction string) error {
	for {
		msg, err := ipc.Recv(conn)
		if err != nil {
			return output.Errorf("daemon recv failed", "")
		}

		var frame ipc.Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			return output.Errorf("invalid daemon response", "")
		}

		switch frame.Type {
		case daemonVerboseFrame:
			recvVerboseFrame(w, frame)
		case "error":
			return output.Errorf(frame.Hint, frame.Action)
		case "result":
			var result ipc.TunnelStartResult
			json.Unmarshal(frame.Payload, &result)
			w.Render(tunnelStartView{result: result, alias: alias, direction: direction})
			return nil
		}
	}
}

func tunnelStartForeground(cmd *cobra.Command, w *output.Writer, alias, direction, localAddr, remoteAddr string) error {
	store := configFrom(cmd.Context())
	host, err := store.Get(alias)
	if err != nil {
		return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts")
	}

	cfg := hostToConnConfigWithStore(host, store)
	cfg.Timeout = 30 * time.Second

	w.Info("connecting to " + alias + "...")
	client, err := sshclient.Dial(cmd.Context(), cfg)
	if err != nil {
		return connErrorToOutput(err, alias)
	}
	defer client.Close()

	tunnelCfg := tunnel.Config{
		Direction:  tunnel.Direction(direction),
		Alias:      alias,
		LocalAddr:  localAddr,
		RemoteAddr: remoteAddr,
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	infoFn := func(msg string) { w.Info(msg) }

	switch tunnelCfg.Direction {
	case tunnel.Local:
		_, err = tunnel.StartLocal(ctx, client, tunnelCfg, infoFn)
	case tunnel.Remote:
		_, err = tunnel.StartRemote(ctx, client, tunnelCfg, infoFn)
	}
	if err != nil {
		return output.Errorf(err.Error(), "")
	}

	w.Info("tunnel running, press Ctrl+C to stop")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	cancel()
	w.Success("tunnel stopped")
	return nil
}

func newTunnelListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List active tunnels",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := ipc.Connect()
			if err != nil {
				w := writerFrom(cmd.Context())
				w.Render(tunnelList(nil))
				return nil
			}
			defer conn.Close()

			env, _ := ipc.MakeEnvelope("tunnel-list", nil)
			if err := ipc.Send(conn, env); err != nil {
				return output.Errorf("daemon send failed", "")
			}

			msg, err := ipc.Recv(conn)
			if err != nil {
				return output.Errorf("daemon recv failed", "")
			}

			var frame ipc.Frame
			json.Unmarshal(msg, &frame)
			if frame.Type == "error" {
				return output.Errorf(frame.Hint, frame.Action)
			}

			var list []tunnel.TunnelInfo
			json.Unmarshal(frame.Payload, &list)

			w := writerFrom(cmd.Context())
			w.Render(tunnelList(list))
			return nil
		},
	}
}

func newTunnelStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <id>",
		Short: "Stop a tunnel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := ipc.Connect()
			if err != nil {
				return output.Errorf("daemon not running", "")
			}
			defer conn.Close()

			env, _ := ipc.MakeEnvelope("tunnel-stop", ipc.TunnelStopPayload{ID: args[0]})
			if err := ipc.Send(conn, env); err != nil {
				return output.Errorf("daemon send failed", "")
			}

			msg, err := ipc.Recv(conn)
			if err != nil {
				return output.Errorf("daemon recv failed", "")
			}

			var frame ipc.Frame
			json.Unmarshal(msg, &frame)
			if frame.Type == "error" {
				return output.Errorf(frame.Hint, frame.Action)
			}

			w := writerFrom(cmd.Context())
			w.Success("stopped " + args[0])
			return nil
		},
	}
}

type tunnelStartView struct {
	result    ipc.TunnelStartResult
	alias     string
	direction string
}

func (v tunnelStartView) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.result)
}

func (v tunnelStartView) Pretty() string {
	arrow := "→"
	if v.direction == "local" {
		return fmt.Sprintf("%s %s %s %s via %s", v.result.ID, v.result.LocalAddr, arrow, v.result.RemoteAddr, v.alias)
	}
	return fmt.Sprintf("%s %s %s %s via %s", v.result.ID, v.result.RemoteAddr, arrow, v.result.LocalAddr, v.alias)
}

type tunnelList []tunnel.TunnelInfo

func (tl tunnelList) Pretty() string {
	if len(tl) == 0 {
		return "no active tunnels"
	}
	var b strings.Builder
	arrow := "→"
	for i, t := range tl {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s %s %s %s %s via %s conns=%d",
			t.ID, t.Direction, t.LocalAddr, arrow, t.RemoteAddr, t.Alias, t.ActiveConn)
	}
	return b.String()
}
