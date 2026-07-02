package appconfig

import "time"

type Config struct {
	Policy PolicyConfig `toml:"policy"`
	Audit  AuditConfig  `toml:"audit"`

	path    string
	modTime time.Time
	exists  bool
}

type PolicyConfig struct {
	Default RuleSet               `toml:"default"`
	Hosts   map[string]HostPolicy `toml:"hosts"`
}

type RuleSet struct {
	Enabled                *bool    `toml:"enabled"`
	CommandWhitelist       []string `toml:"command_whitelist"`
	CommandBlacklist       []string `toml:"command_blacklist"`
	LocalPathWhitelist     []string `toml:"local_path_whitelist"`
	RemotePathWhitelist    []string `toml:"remote_path_whitelist"`
	LocalForwardWhitelist  []string `toml:"local_forward_whitelist"`
	RemoteForwardWhitelist []string `toml:"remote_forward_whitelist"`
}

type HostPolicy struct {
	RuleSet
	Mode string `toml:"mode"`
}

type AuditConfig struct {
	Enabled *bool  `toml:"enabled"`
	Path    string `toml:"path"`
	MaxSize string `toml:"max_size"`
}

func (c *Config) Path() string {
	if c == nil {
		return ""
	}
	return c.path
}

func (c *Config) Exists() bool {
	return c != nil && c.exists
}

func Bool(v bool) *bool {
	return &v
}
