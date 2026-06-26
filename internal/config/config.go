package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ssh_config "github.com/kevinburke/ssh_config"
)

type Host struct {
	Alias        string
	HostName     string
	User         string
	Port         string
	IdentityFile string
	ProxyJump    string
	Metadata     map[string]string
}

type Store struct {
	cfg   *ssh_config.Config
	raw   []byte
	hosts []Host
}

func Load(path string) (*Store, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open ssh config: %w", err)
	}
	defer f.Close()

	cfg, err := ssh_config.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("parse ssh config: %w", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ssh config: %w", err)
	}

	s := &Store{cfg: cfg, raw: raw}
	s.hosts = s.extractHosts()
	return s, nil
}

func LoadDefault(configFlag string) (*Store, error) {
	path := configFlag
	if path == "" {
		path = os.Getenv("SSHQ_CONFIG")
	}
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("home directory: %w", err)
		}
		path = filepath.Join(home, ".ssh", "config")
	}
	return Load(path)
}

func (s *Store) List() []Host {
	result := make([]Host, len(s.hosts))
	copy(result, s.hosts)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Alias < result[j].Alias
	})
	return result
}

func (s *Store) Get(alias string) (Host, error) {
	for _, h := range s.hosts {
		if h.Alias == alias {
			return h, nil
		}
	}
	return Host{}, fmt.Errorf("host %q not found", alias)
}

func (s *Store) Search(pattern string) []Host {
	pattern = strings.ToLower(pattern)
	var matches []Host
	for _, h := range s.hosts {
		if strings.Contains(strings.ToLower(h.Alias), pattern) ||
			strings.Contains(strings.ToLower(h.HostName), pattern) ||
			strings.Contains(strings.ToLower(h.Metadata["description"]), pattern) {
			matches = append(matches, h)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Alias < matches[j].Alias
	})
	return matches
}

func (s *Store) extractHosts() []Host {
	metaMap := parseMetadataBlocks(s.raw)
	var hosts []Host

	for _, host := range s.cfg.Hosts {
		for _, pattern := range host.Patterns {
			name := pattern.String()
			if name == "*" || strings.ContainsAny(name, "*?") {
				continue
			}

			h := Host{
				Alias:        name,
				HostName:     s.get(name, "HostName"),
				User:         s.get(name, "User"),
				Port:         s.get(name, "Port"),
				IdentityFile: s.get(name, "IdentityFile"),
				ProxyJump:    s.get(name, "ProxyJump"),
				Metadata:     make(map[string]string),
			}

			if h.HostName == "" {
				h.HostName = name
			}
			if h.Port == "" {
				h.Port = "22"
			}

			if meta, ok := metaMap[name]; ok {
				for k, v := range meta {
					h.Metadata[k] = v
				}
			}

			hosts = append(hosts, h)
		}
	}
	return hosts
}

func (s *Store) get(alias, key string) string {
	val, err := s.cfg.Get(alias, key)
	if err != nil {
		return ""
	}
	return val
}
