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

	"github.com/shayuc137/sshq/internal/sshclient"
)

const DefaultTTL = 24 * time.Hour
const CachePathEnv = "SSHQ_PROFILE_CACHE"

type CacheOption func(*cacheOptions)

type cacheOptions struct {
	path string
	info func(string)
}

type Cache struct {
	path string
	ttl  time.Duration
	info func(string)
	mu   sync.RWMutex
	data map[string]*Profile
}

func WithCachePath(path string) CacheOption {
	return func(opts *cacheOptions) { opts.path = path }
}

func WithCacheInfo(info func(string)) CacheOption {
	return func(opts *cacheOptions) { opts.info = info }
}

func NewCache(ttl time.Duration, opts ...CacheOption) (*Cache, error) {
	cfg := cacheOptions{path: os.Getenv(CachePathEnv)}
	for _, opt := range opts {
		opt(&cfg)
	}

	path := cfg.path
	home, err := os.UserHomeDir()
	if err != nil && path == "" {
		return nil, err
	}
	if path == "" {
		path = filepath.Join(home, ".config", "sshq", "cache", "profiles.json")
	}

	c := &Cache{
		path: path,
		ttl:  ttl,
		info: cfg.info,
		data: make(map[string]*Profile),
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		c.warn("profile cache directory unavailable: " + err.Error())
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
		if !os.IsNotExist(err) {
			c.warn("profile cache load failed: " + err.Error())
		}
		return
	}

	loaded := make(map[string]*Profile)
	if err := json.Unmarshal(data, &loaded); err != nil {
		c.warn("profile cache is corrupt, rebuilding: " + err.Error())
		c.mu.Lock()
		c.data = make(map[string]*Profile)
		c.mu.Unlock()
		c.Save()
		return
	}
	if loaded == nil {
		loaded = make(map[string]*Profile)
	}

	c.mu.Lock()
	c.data = loaded
	c.mu.Unlock()
}

func (c *Cache) Save() {
	c.mu.RLock()
	data, err := json.MarshalIndent(c.data, "", "  ")
	c.mu.RUnlock()
	if err != nil {
		c.warn("profile cache encode failed: " + err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		c.warn("profile cache directory unavailable: " + err.Error())
		return
	}
	if err := os.WriteFile(c.path, data, 0644); err != nil {
		c.warn("profile cache save failed: " + err.Error())
	}
}

func (c *Cache) warn(msg string) {
	if c.info != nil {
		c.info(msg)
	}
}

func GetProfile(ctx context.Context, client *sshclient.Client, cache *Cache, host, port string) (*Profile, error) {
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
