package sshclient

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

type ConnConfig struct {
	Host         string
	Port         string
	User         string
	IdentityFile string
	Timeout      time.Duration
}

func Dial(ctx context.Context, cfg ConnConfig) (*ssh.Client, error) {
	methods, err := authMethods(cfg)
	if err != nil {
		return nil, fmt.Errorf("auth setup: %w", err)
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("no authentication methods available for %s@%s", cfg.User, cfg.Host)
	}

	hostKeyCallback, err := hostKeyCallback()
	if err != nil {
		return nil, fmt.Errorf("host key verification: %w", err)
	}

	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            methods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         cfg.Timeout,
	}

	addr := net.JoinHostPort(cfg.Host, cfg.Port)

	dialer := net.Dialer{Timeout: cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}

	deadline, ok := ctx.Deadline()
	if ok {
		conn.SetDeadline(deadline)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		conn.Close()
		return nil, categorizeError(err, cfg)
	}

	if ok {
		conn.SetDeadline(time.Time{})
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}

func authMethods(cfg ConnConfig) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if m := agentAuth(); m != nil {
		methods = append(methods, m)
	}

	if cfg.IdentityFile != "" {
		m, err := keyAuth(cfg.IdentityFile)
		if err == nil {
			methods = append(methods, m)
		}
	}

	for _, name := range []string{"id_ed25519", "id_rsa"} {
		home, err := os.UserHomeDir()
		if err != nil {
			continue
		}
		path := filepath.Join(home, ".ssh", name)
		if path == cfg.IdentityFile {
			continue
		}
		m, err := keyAuth(path)
		if err == nil {
			methods = append(methods, m)
		}
	}

	return methods, nil
}

func agentAuth() ssh.AuthMethod {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil
	}
	return ssh.PublicKeysCallback(agent.NewClient(conn).Signers)
}

func keyAuth(path string) (ssh.AuthMethod, error) {
	if path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, path[2:])
	}

	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse key %s: %w", path, err)
	}

	return ssh.PublicKeys(signer), nil
}

func hostKeyCallback() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".ssh", "known_hosts")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("known_hosts not found at %s — sshq requires strict host key verification", path)
	}
	return knownhosts.New(path)
}

func categorizeError(err error, cfg ConnConfig) error {
	if _, ok := err.(*net.OpError); ok {
		return fmt.Errorf("network error connecting to %s:%s: %w", cfg.Host, cfg.Port, err)
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "unable to authenticate") || strings.Contains(errMsg, "no supported methods") {
		return fmt.Errorf("authentication failed for %s@%s:%s: %w", cfg.User, cfg.Host, cfg.Port, err)
	}
	if strings.Contains(errMsg, "host key") || strings.Contains(errMsg, "knownhosts") {
		return fmt.Errorf("host key verification failed for %s:%s: %w", cfg.Host, cfg.Port, err)
	}
	return fmt.Errorf("SSH handshake with %s:%s: %w", cfg.Host, cfg.Port, err)
}

// strings.Contains used directly — removed custom contains/searchString
