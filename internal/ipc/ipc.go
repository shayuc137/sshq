package ipc

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
)

const MaxMessageSize = 10 * 1024 * 1024 // 10MB

func SocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/sshq-daemon.sock"
	}
	dir := filepath.Join(home, ".config", "sshq")
	os.MkdirAll(dir, 0700)
	return filepath.Join(dir, "daemon.sock")
}

func PIDPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/sshq-daemon.pid"
	}
	return filepath.Join(home, ".config", "sshq", "daemon.pid")
}

func Send(conn net.Conn, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(data)))
	if _, err := conn.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	return nil
}

func Recv(conn net.Conn) (json.RawMessage, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	length := binary.BigEndian.Uint32(header)
	if length > MaxMessageSize {
		return nil, fmt.Errorf("message too large: %d bytes", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return json.RawMessage(body), nil
}

func Connect() (net.Conn, error) {
	return net.Dial("unix", SocketPath())
}

func IsRunning() bool {
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
