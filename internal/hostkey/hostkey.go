package hostkey

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Status int

const (
	Trusted  Status = iota
	Unknown         // host not in known_hosts
	Mismatch        // key exists but differs
)

type Result struct {
	Status Status
	Key    ssh.PublicKey
	Want   []knownhosts.KnownKey
	Addr   string
}

var errKeyCapture = errors.New("host key captured")

const KnownHostsPathEnv = "SSHQ_KNOWN_HOSTS"

// FetchConn captures the host key during an SSH probe handshake over conn. The
// caller owns conn and remains responsible for closing it.
func FetchConn(conn net.Conn, addr string, timeout time.Duration) (ssh.PublicKey, error) {
	var hostKey ssh.PublicKey
	cfg := &ssh.ClientConfig{
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			hostKey = key
			return errKeyCapture
		},
		Timeout: timeout,
	}

	// Close-on-timeout instead of SetDeadline: conns tunneled through a
	// ProxyJump hop are ssh channels, which reject deadlines
	// ("tcpChan: deadline not supported").
	if timeout > 0 {
		guard := time.AfterFunc(timeout, func() { conn.Close() })
		defer guard.Stop()
	}

	handshakeErr := fetchConnHandshake(conn, addr, cfg)

	if hostKey == nil {
		if handshakeErr != nil {
			return nil, fmt.Errorf("fetch host key from %s: %w", addr, handshakeErr)
		}
		return nil, fmt.Errorf("no host key received from %s", addr)
	}
	return hostKey, nil
}

var fetchConnHandshake = func(conn net.Conn, addr string, cfg *ssh.ClientConfig) error {
	_, _, _, err := ssh.NewClientConn(conn, addr, cfg)
	return err
}

func Check(addr string, key ssh.PublicKey) (*Result, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}

	normalized := knownhosts.Normalize(addr)
	result := &Result{Key: key, Addr: normalized}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		result.Status = Unknown
		return result, nil
	}

	cb, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("parse known_hosts: %w", err)
	}

	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	remote := &net.TCPAddr{IP: net.ParseIP(host), Port: port}

	err = cb(addr, remote, key)
	if err == nil {
		result.Status = Trusted
		return result, nil
	}

	var keyErr *knownhosts.KeyError
	if errors.As(err, &keyErr) {
		if len(keyErr.Want) == 0 {
			result.Status = Unknown
		} else {
			result.Status = Mismatch
			result.Want = keyErr.Want
		}
		return result, nil
	}

	return nil, err
}

func Add(addr string, key ssh.PublicKey) error {
	path, err := Path()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create .ssh directory: %w", err)
	}

	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, key)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open known_hosts: %w", err)
	}
	defer f.Close()

	_, err = fmt.Fprintln(f, line)
	return err
}

func Remove(addr string) (int, error) {
	path, err := Path()
	if err != nil {
		return 0, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	normalized := knownhosts.Normalize(addr)
	lines := strings.Split(string(data), "\n")
	var kept []string
	removed := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			kept = append(kept, line)
			continue
		}
		if matchesHost(trimmed, normalized) {
			removed++
			continue
		}
		kept = append(kept, line)
	}

	if removed == 0 {
		return 0, nil
	}

	result := strings.Join(kept, "\n")
	result = strings.TrimRight(result, "\n") + "\n"
	return removed, os.WriteFile(path, []byte(result), 0600)
}

func matchesHost(line, normalized string) bool {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return false
	}
	if strings.HasPrefix(fields[0], "|") {
		return false
	}
	for _, h := range strings.Split(fields[0], ",") {
		if h == normalized {
			return true
		}
	}
	return false
}

func Fingerprint(key ssh.PublicKey) string {
	return ssh.FingerprintSHA256(key)
}

func KeyType(key ssh.PublicKey) string {
	return key.Type()
}

// LookupKeys returns the known_hosts names relevant to an alias and its
// resolved endpoint, preserving order and removing duplicates.
func LookupKeys(alias, hostname, port string) []string {
	keys := make([]string, 0, 3)
	seen := make(map[string]struct{}, 3)
	for _, key := range []string{alias, hostname, knownhosts.Normalize(net.JoinHostPort(hostname, port))} {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func Path() (string, error) {
	if path := os.Getenv(KnownHostsPathEnv); path != "" {
		return expandHome(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home directory: %w", err)
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

func expandHome(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home directory: %w", err)
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home directory: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
