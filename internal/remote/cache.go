package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const DefaultTTL = 24 * time.Hour

type Cache struct {
	path string
	ttl  time.Duration
	mu   sync.RWMutex
	data map[string]*Profile
}

func NewCache(ttl time.Duration) (*Cache, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".config", "sshq", "cache")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	c := &Cache{
		path: filepath.Join(dir, "profiles.json"),
		ttl:  ttl,
		data: make(map[string]*Profile),
	}
	c.Load()
	return c, nil
}

func (c *Cache) key(host, port string) string {
	return net.JoinHostPort(host, port)
}

func (c *Cache) Get(host, port string) (*Profile, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.data[c.key(host, port)]
	if !ok {
		return nil, false
	}
	if p.Age() > c.ttl {
		return nil, false
	}
	return p, true
}

func (c *Cache) Put(host, port string, p *Profile) {
	c.mu.Lock()
	c.data[c.key(host, port)] = p
	c.mu.Unlock()
	c.Save()
}

func (c *Cache) Invalidate(host, port string) {
	c.mu.Lock()
	delete(c.data, c.key(host, port))
	c.mu.Unlock()
	c.Save()
}

func (c *Cache) Load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	json.Unmarshal(data, &c.data)
}

func (c *Cache) Save() {
	c.mu.RLock()
	data, err := json.MarshalIndent(c.data, "", "  ")
	c.mu.RUnlock()
	if err != nil {
		return
	}
	os.WriteFile(c.path, data, 0644)
}

func GetProfile(ctx context.Context, client *ssh.Client, cache *Cache, host, port string) (*Profile, error) {
	if cache != nil {
		if p, ok := cache.Get(host, port); ok {
			return p, nil
		}
	}
	p, err := Detect(ctx, client)
	if err != nil {
		return &Profile{
			OS:         Unknown,
			Shell:      Sh,
			DetectedAt: time.Now().Unix(),
		}, fmt.Errorf("profile detection failed: %w", err)
	}
	if cache != nil {
		cache.Put(host, port, p)
	}
	return p, nil
}
