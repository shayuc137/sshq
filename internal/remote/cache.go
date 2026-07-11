package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	fileMtime    time.Time
	fileSize     int64
	statWarnOnce sync.Once
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
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maybeReload()
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
	defer c.mu.Unlock()
	c.maybeReload()
	c.data[c.key(host, port)] = p
	c.saveLocked()
}

func (c *Cache) Invalidate(host, port string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maybeReload()
	delete(c.data, c.key(host, port))
	c.saveLocked()
}

func (c *Cache) Clear() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maybeReload()
	count := len(c.data)
	c.data = make(map[string]*Profile)
	c.saveLocked()
	return count
}

func (c *Cache) Load() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
}

func (c *Cache) loadLocked() {
	f, err := os.Open(c.path)
	if err != nil {
		if !os.IsNotExist(err) {
			c.warn("profile cache load failed: " + err.Error())
		}
		return
	}
	data, readErr := io.ReadAll(f)
	info, statErr := f.Stat()
	closeErr := f.Close()
	if readErr != nil {
		c.warn("profile cache load failed: " + readErr.Error())
		return
	}
	if statErr != nil {
		c.warn("profile cache stat failed: " + statErr.Error())
		return
	}
	if closeErr != nil {
		c.warn("profile cache load failed: " + closeErr.Error())
		return
	}

	loaded := make(map[string]*Profile)
	if err := json.Unmarshal(data, &loaded); err != nil {
		c.warn("profile cache is corrupt, rebuilding: " + err.Error())
		c.data = make(map[string]*Profile)
		c.saveLocked()
		return
	}
	if loaded == nil {
		loaded = make(map[string]*Profile)
	}

	c.data = loaded
	c.fileMtime = info.ModTime()
	c.fileSize = info.Size()
}

func (c *Cache) Save() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.saveLocked()
}

func (c *Cache) saveLocked() {
	data, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		c.warn("profile cache encode failed: " + err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		c.warn("profile cache directory unavailable: " + err.Error())
		return
	}

	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".sshq-profiles-*.tmp")
	if err != nil {
		c.warn("profile cache save failed: " + err.Error())
		return
	}
	tmpPath := tmp.Name()
	removeTemp := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}
	if _, err := tmp.Write(data); err != nil {
		removeTemp()
		c.warn("profile cache save failed: " + err.Error())
		return
	}
	if err := tmp.Sync(); err != nil {
		removeTemp()
		c.warn("profile cache save failed: " + err.Error())
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		c.warn("profile cache save failed: " + err.Error())
		return
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		os.Remove(tmpPath)
		c.warn("profile cache save failed: " + err.Error())
		return
	}
	// Stat the temp file before rename: rename preserves mtime, and statting
	// the destination afterwards could record a concurrent writer's version,
	// masking their update from the next maybeReload.
	info, statErr := os.Stat(tmpPath)
	if err := os.Rename(tmpPath, c.path); err != nil {
		os.Remove(tmpPath)
		c.warn("profile cache save failed: " + err.Error())
		return
	}
	if statErr != nil {
		c.warnStat(statErr)
		return
	}
	c.fileMtime = info.ModTime()
	c.fileSize = info.Size()
}

// maybeReload requires c.mu to be held so writes remain a single transaction.
func (c *Cache) maybeReload() {
	info, err := os.Stat(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Disk is the source of truth: a missing file means an empty
			// cache (never created, or removed out from under us).
			if len(c.data) > 0 {
				c.data = make(map[string]*Profile)
			}
			c.fileMtime = time.Time{}
			c.fileSize = 0
			return
		}
		c.warnStat(err)
		return
	}
	if !info.ModTime().Equal(c.fileMtime) || info.Size() != c.fileSize {
		c.loadLocked()
	}
}

func (c *Cache) warnStat(err error) {
	c.statWarnOnce.Do(func() {
		c.warn("profile cache stat failed: " + err.Error())
	})
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
