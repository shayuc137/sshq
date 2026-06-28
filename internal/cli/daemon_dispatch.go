package cli

import (
	"net"

	"github.com/shayuc137/sshq/internal/ipc"
)

type daemonRecvFunc func(net.Conn) error
type daemonFallbackFunc func(reason string) error

func daemonDispatch(envelope ipc.Envelope, recvFn daemonRecvFunc, fallback daemonFallbackFunc) error {
	conn, err := ipc.Connect()
	if err != nil {
		return fallback("daemon unreachable")
	}
	defer conn.Close()

	if err := ipc.Send(conn, envelope); err != nil {
		return fallback("daemon send failed")
	}

	return recvFn(conn)
}
