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
	return fmt.Sprintf("%s:%s:%s:%s", cfg.Host, cfg.Port, cfg.User, cfg.IdentityFile)
}

func (p *Pool) Get(ctx context.Context, alias string, cfg sshclient.ConnConfig) (*ssh.Client, error) {
	key := Key(cfg)

	p.mu.Lock()
	e, ok := p.conns[key]
	if ok && isHealthy(e.client) {
		e.lastUsed = time.Now()
		e.alias = alias
		p.mu.Unlock()
		return e.client, nil
	}
	if ok {
		e.client.Close()
		delete(p.conns, key)
	}
	p.mu.Unlock()

	client, err := sshclient.Dial(ctx, cfg)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
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

func isHealthy(client *ssh.Client) bool {
	_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}
