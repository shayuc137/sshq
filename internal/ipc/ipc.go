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

// ParseEnvelope decodes a raw message into a v2 Envelope.
func ParseEnvelope(raw json.RawMessage) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, fmt.Errorf("parse envelope: %w", err)
	}
	if env.Action == "" {
		return Envelope{}, fmt.Errorf("envelope missing action field")
	}
	return env, nil
}

// DetectV1 checks whether a raw message is a v1-format request.
func DetectV1(raw json.RawMessage) bool {
	var probe v1Request
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.ProtocolVersion > 0
}

// SendError sends an error frame with hint and action.
func SendError(conn net.Conn, hint, action string) error {
	return Send(conn, Frame{Type: "error", Hint: hint, Action: action})
}

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
