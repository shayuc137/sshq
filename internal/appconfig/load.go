package appconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
	"github.com/shayuc137/sshq/internal/credential"
)

const FileName = "config.toml"

func DefaultPath() (string, error) {
	dir, err := credential.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

func Load() (*Config, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

func LoadFrom(path string) (*Config, error) {
	stat, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{path: path}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat app config: %w", err)
	}
	if stat.IsDir() {
		return nil, fmt.Errorf("app config path is a directory: %s", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open app config: %w", err)
	}
	defer f.Close()

	cfg := &Config{path: path, modTime: stat.ModTime(), exists: true}
	dec := toml.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse app config: %w", err)
	}
	if cfg.Policy.Hosts == nil {
		cfg.Policy.Hosts = make(map[string]HostPolicy)
	}
	return cfg, nil
}

func (c *Config) ReloadIfChanged() (bool, error) {
	if c == nil {
		return false, nil
	}

	stat, err := os.Stat(c.path)
	if errors.Is(err, os.ErrNotExist) {
		if !c.exists {
			return false, nil
		}
		*c = Config{path: c.path}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat app config: %w", err)
	}
	if stat.IsDir() {
		return false, fmt.Errorf("app config path is a directory: %s", c.path)
	}
	if c.exists && stat.ModTime().Equal(c.modTime) {
		return false, nil
	}

	next, err := LoadFrom(c.path)
	if err != nil {
		return false, err
	}
	*c = *next
	return true, nil
}
