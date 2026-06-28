package pool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shayuc137/sshq/internal/sshclient"
	"golang.org/x/crypto/ssh"
)

type Pool struct {
	mu    sync.Mutex
	conns map[string]*entry
	ttl   time.Duration
}

type entry struct {
	client   *ssh.Client
	cfg      sshclient.ConnConfig
	alias    string
	lastUsed time.Time
}

type ConnInfo struct {
	Key       string    `json:"key"`
	Alias     string    `json:"alias"`
	Host      string    `json:"host"`
	Port      string    `json:"port"`
	IdleSince time.Time `json:"idle_since"`
}

func New(ttl time.Duration) *Pool {
	return &Pool{
		conns: make(map[string]*entry),
		ttl:   ttl,
	}
}

func Key(cfg sshclient.ConnConfig) string {
	key := fmt.Sprintf("%s:%s:%s:%s", cfg.Host, cfg.Port, cfg.User, cfg.IdentityFile)
	if cfg.ProxyJump != "" {
		key += ":proxy=" + cfg.ProxyJump
	}
	return key
}

func (p *Pool) Get(ctx context.Context, alias string, cfg sshclient.ConnConfig) (*ssh.Client, error) {
	key := Key(cfg)

	// Phase 1: look up the cached entry under the lock, then release it. The
	// health check below performs network I/O (an SSH keepalive round-trip) and
	// must never run while holding p.mu — otherwise a slow or hung connection
	// would stall every other pool operation, which is exactly what concurrent
	// cluster fan-out triggers.
	p.mu.Lock()
	e, ok := p.conns[key]
	p.mu.Unlock()

	if ok {
		if healthFn(e.client) {
			// Phase 2: re-acquire the lock and make sure the entry we just
			// health-checked is still the one in the map. Another goroutine may
			// have reaped or replaced it while we were off-lock; if so, discard
			// this result and fall through to dial a fresh connection.
			p.mu.Lock()
			if cur, still := p.conns[key]; still && cur == e {
				cur.lastUsed = time.Now()
				cur.alias = alias
				p.mu.Unlock()
				return cur.client, nil
			}
			p.mu.Unlock()
		} else {
			// Unhealthy: drop it from the map (only if it is still the same
			// entry) and close it off-lock. If someone else already replaced it,
			// they own the new connection's lifecycle and we must not touch it.
			p.mu.Lock()
			removed := false
			if cur, still := p.conns[key]; still && cur == e {
				delete(p.conns, key)
				removed = true
			}
			p.mu.Unlock()
			if removed {
				e.client.Close()
			}
		}
	}

	client, err := dialFn(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// A concurrent Get may have dialed the same key while we were dialing. If an
	// entry already exists, reuse it and close our redundant connection so we do
	// not leak a TCP connection or orphan the map entry.
	p.mu.Lock()
	if cur, exists := p.conns[key]; exists {
		cur.lastUsed = time.Now()
		cur.alias = alias
		p.mu.Unlock()
		client.Close()
		return cur.client, nil
	}
	p.conns[key] = &entry{
		client:   client,
		cfg:      cfg,
		alias:    alias,
		lastUsed: time.Now(),
	}
	p.mu.Unlock()

	return client, nil
}

func (p *Pool) Close(key string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	e, ok := p.conns[key]
	if !ok {
		return nil
	}
	delete(p.conns, key)
	return e.client.Close()
}

func (p *Pool) CloseAll() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var lastErr error
	for key, e := range p.conns {
		if err := e.client.Close(); err != nil {
			lastErr = err
		}
		delete(p.conns, key)
	}
	return lastErr
}

func (p *Pool) Stats() []ConnInfo {
	p.mu.Lock()
	defer p.mu.Unlock()

	stats := make([]ConnInfo, 0, len(p.conns))
	for key, e := range p.conns {
		stats = append(stats, ConnInfo{
			Key:       key,
			Alias:     e.alias,
			Host:      e.cfg.Host,
			Port:      e.cfg.Port,
			IdleSince: e.lastUsed,
		})
	}
	return stats
}

func (p *Pool) Reap() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	reaped := 0
	for key, e := range p.conns {
		if now.Sub(e.lastUsed) > p.ttl {
			e.client.Close()
			delete(p.conns, key)
			reaped++
		}
	}
	return reaped
}

func (p *Pool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.conns)
}

// healthFn and dialFn are package-level seams so the concurrency behaviour of
// Get can be exercised deterministically in tests without a real SSH server.
// Production always uses the real implementations below.
var (
	healthFn = isHealthy
	dialFn   = sshclient.Dial
)

func isHealthy(client *ssh.Client) bool {
	_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}
