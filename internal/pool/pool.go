package pool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shayuc137/sshq/internal/sshclient"
)

// DialFunc and HealthFunc are the pool's dependency-injection seams. They default
// to the real implementations in New and let tests drive Get's concurrency
// behaviour deterministically without a live SSH server — replacing the former
// package-level mutable globals, which leaked shared state across tests.
type DialFunc func(context.Context, sshclient.ConnConfig) (*sshclient.Client, error)
type HealthFunc func(*sshclient.Client) bool

type Pool struct {
	mu    sync.Mutex
	conns map[string]*entry
	ttl   time.Duration

	DialFunc   DialFunc
	HealthFunc HealthFunc
}

type entry struct {
	client   *sshclient.Client
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
		conns:      make(map[string]*entry),
		ttl:        ttl,
		DialFunc:   sshclient.Dial,
		HealthFunc: isHealthy,
	}
}

func Key(cfg sshclient.ConnConfig) string {
	key := fmt.Sprintf("%s:%s:%s:%s", cfg.Host, cfg.Port, cfg.User, cfg.IdentityFile)
	if cfg.ProxyJump != "" {
		key += ":proxy=" + cfg.ProxyJump
	}
	return key
}

func (p *Pool) Get(ctx context.Context, alias string, cfg sshclient.ConnConfig) (*sshclient.Client, error) {
	client, _, err := p.GetWithStatus(ctx, alias, cfg)
	return client, err
}

func (p *Pool) GetWithStatus(ctx context.Context, alias string, cfg sshclient.ConnConfig) (*sshclient.Client, bool, error) {
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
		if p.HealthFunc(e.client) {
			// Phase 2: re-acquire the lock and make sure the entry we just
			// health-checked is still the one in the map. Another goroutine may
			// have reaped or replaced it while we were off-lock; if so, discard
			// this result and fall through to dial a fresh connection.
			p.mu.Lock()
			if cur, still := p.conns[key]; still && cur == e {
				cur.lastUsed = time.Now()
				cur.client.Alias = alias
				p.mu.Unlock()
				return cur.client, true, nil
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

	client, err := p.DialFunc(ctx, cfg)
	if err != nil {
		return nil, false, err
	}
	client.Alias = alias

	// A concurrent Get may have dialed the same key while we were dialing. If an
	// entry already exists, reuse it and close our redundant connection so we do
	// not leak a TCP connection or orphan the map entry.
	p.mu.Lock()
	if cur, exists := p.conns[key]; exists {
		cur.lastUsed = time.Now()
		cur.client.Alias = alias
		p.mu.Unlock()
		client.Close()
		return cur.client, true, nil
	}
	p.conns[key] = &entry{
		client:   client,
		lastUsed: time.Now(),
	}
	p.mu.Unlock()

	return client, false, nil
}

func (p *Pool) Close(key string) error {
	// Remove the entry under the lock, then close it off-lock: client.Close
	// performs network I/O (and cascades through any ProxyJump chain), which must
	// never run while holding p.mu or it would stall every other pool operation.
	p.mu.Lock()
	e, ok := p.conns[key]
	if ok {
		delete(p.conns, key)
	}
	p.mu.Unlock()

	if !ok {
		return nil
	}
	return e.client.Close()
}

func (p *Pool) CloseAll() error {
	p.mu.Lock()
	clients := make([]*sshclient.Client, 0, len(p.conns))
	for key, e := range p.conns {
		clients = append(clients, e.client)
		delete(p.conns, key)
	}
	p.mu.Unlock()

	var lastErr error
	for _, c := range clients {
		if err := c.Close(); err != nil {
			lastErr = err
		}
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
			Alias:     e.client.Alias,
			Host:      e.client.Config.Host,
			Port:      e.client.Config.Port,
			IdleSince: e.lastUsed,
		})
	}
	return stats
}

func (p *Pool) Reap() int {
	// Collect expired entries under the lock, then close them off-lock for the
	// same reason as Close: never hold p.mu across the client's network I/O.
	p.mu.Lock()
	now := time.Now()
	var expired []*sshclient.Client
	for key, e := range p.conns {
		if now.Sub(e.lastUsed) > p.ttl {
			expired = append(expired, e.client)
			delete(p.conns, key)
		}
	}
	p.mu.Unlock()

	for _, c := range expired {
		c.Close()
	}
	return len(expired)
}

func (p *Pool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.conns)
}

func isHealthy(client *sshclient.Client) bool {
	_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}
