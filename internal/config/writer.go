package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ssh_config "github.com/kevinburke/ssh_config"
)

func (s *Store) Path() string { return s.path }

func (s *Store) Add(h Host) error {
	if _, err := s.Get(h.Alias); err == nil {
		return fmt.Errorf("host %q already exists", h.Alias)
	}
	if h.HostName == "" {
		return fmt.Errorf("hostname is required")
	}

	block := buildHostBlock(h)

	raw := s.raw
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		raw = append(raw, '\n')
	}
	raw = append(raw, []byte(block)...)
	return s.reload(raw)
}

func (s *Store) Remove(alias string) error {
	if _, err := s.Get(alias); err != nil {
		return err
	}

	raw := removeHostBlock(s.raw, alias)
	return s.reload(raw)
}

func (s *Store) Set(alias, key, value string) error {
	if _, err := s.Get(alias); err != nil {
		return err
	}

	sshKeys := map[string]bool{
		"hostname": true, "user": true, "port": true,
		"identityfile": true, "proxyjump": true,
	}

	if sshKeys[strings.ToLower(key)] {
		raw := setSSHValue(s.raw, alias, key, value)
		return s.reload(raw)
	}

	raw := setMetadataValue(s.raw, alias, key, value)
	return s.reload(raw)
}

func (s *Store) Save() error {
	if s.path == "" {
		return fmt.Errorf("no config path set")
	}

	bak := s.path + ".bak"
	if data, err := os.ReadFile(s.path); err == nil {
		os.WriteFile(bak, data, 0600)
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".sshq-config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(s.raw); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	tmp.Close()

	if err := os.Chmod(tmpPath, 0600); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

func (s *Store) reload(raw []byte) error {
	ns, err := loadFromBytes(raw, s.path)
	if err != nil {
		return fmt.Errorf("resulting config is invalid: %w", err)
	}
	s.cfg = ns.cfg
	s.raw = ns.raw
	s.hosts = ns.hosts
	return nil
}

func loadFromBytes(raw []byte, path string) (*Store, error) {
	cfg, err := decodeBytes(raw)
	if err != nil {
		return nil, err
	}
	s := &Store{cfg: cfg, raw: raw, path: path}
	s.hosts = s.extractHosts()
	return s, nil
}

func decodeBytes(raw []byte) (*ssh_config.Config, error) {
	return ssh_config.DecodeBytes(raw)
}

func buildHostBlock(h Host) string {
	var b strings.Builder

	b.WriteString("\n")

	hasMeta := len(h.Metadata) > 0
	if hasMeta {
		b.WriteString(fmt.Sprintf("# ===== %s =====\n", h.Alias))
		keys := make([]string, 0, len(h.Metadata))
		for k := range h.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("# sshq:%s=%s\n", k, h.Metadata[k]))
		}
	}

	b.WriteString(fmt.Sprintf("Host %s\n", h.Alias))
	b.WriteString(fmt.Sprintf("    HostName %s\n", h.HostName))
	if h.User != "" {
		b.WriteString(fmt.Sprintf("    User %s\n", h.User))
	}
	if h.Port != "" && h.Port != "22" {
		b.WriteString(fmt.Sprintf("    Port %s\n", h.Port))
	}
	if h.IdentityFile != "" {
		b.WriteString(fmt.Sprintf("    IdentityFile %s\n", h.IdentityFile))
	}
	if h.ProxyJump != "" {
		b.WriteString(fmt.Sprintf("    ProxyJump %s\n", h.ProxyJump))
	}

	return b.String()
}

func removeHostBlock(raw []byte, alias string) []byte {
	lines := strings.Split(string(raw), "\n")
	hostLine := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Host ") {
			fields := strings.Fields(trimmed)
			if len(fields) == 2 && fields[1] == alias {
				hostLine = i
				break
			}
		}
	}

	if hostLine < 0 {
		return raw
	}

	// Find metadata comment block above
	start := hostLine
	for start > 0 {
		prev := strings.TrimSpace(lines[start-1])
		if strings.HasPrefix(prev, "#") {
			start--
		} else if prev == "" && start-2 >= 0 && strings.HasPrefix(strings.TrimSpace(lines[start-2]), "#") {
			start--
		} else if prev == "" {
			start--
		} else {
			break
		}
	}

	// Find end of Host block (next Host/Match or EOF)
	end := hostLine + 1
	for end < len(lines) {
		trimmed := strings.TrimSpace(lines[end])
		if strings.HasPrefix(trimmed, "Host ") || strings.HasPrefix(trimmed, "Match ") {
			break
		}
		if trimmed == "" {
			end++
			continue
		}
		if strings.HasPrefix(trimmed, "#") && end > hostLine {
			// Comment after indented block might belong to next host
			break
		}
		end++
	}

	// Trim trailing empty lines from removed block
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}

	result := make([]string, 0, len(lines)-(end-start))
	result = append(result, lines[:start]...)
	result = append(result, lines[end:]...)

	return []byte(strings.Join(result, "\n"))
}

func setSSHValue(raw []byte, alias, key, value string) []byte {
	lines := strings.Split(string(raw), "\n")
	inHost := false
	canonKey := canonicalSSHKey(key)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Host ") {
			fields := strings.Fields(trimmed)
			inHost = len(fields) == 2 && fields[1] == alias
			continue
		}
		if inHost && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 1 && strings.EqualFold(parts[0], canonKey) {
				lines[i] = fmt.Sprintf("    %s %s", canonKey, value)
				return []byte(strings.Join(lines, "\n"))
			}
		}
		if inHost && (trimmed == "" || strings.HasPrefix(trimmed, "Host ") || strings.HasPrefix(trimmed, "Match ")) {
			// Key not found in block, insert before this line
			insert := fmt.Sprintf("    %s %s", canonKey, value)
			result := make([]string, 0, len(lines)+1)
			result = append(result, lines[:i]...)
			result = append(result, insert)
			result = append(result, lines[i:]...)
			return []byte(strings.Join(result, "\n"))
		}
	}

	// Host was last block, append
	if inHost {
		lines = append(lines, fmt.Sprintf("    %s %s", canonKey, value))
		return []byte(strings.Join(lines, "\n"))
	}

	return raw
}

func setMetadataValue(raw []byte, alias, key, value string) []byte {
	lines := strings.Split(string(raw), "\n")

	hostLine := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Host ") {
			fields := strings.Fields(trimmed)
			if len(fields) == 2 && fields[1] == alias {
				hostLine = i
				break
			}
		}
	}
	if hostLine < 0 {
		return raw
	}

	// Look for existing sshq:key= in comments above Host line
	prefix := fmt.Sprintf("# sshq:%s=", key)
	for i := hostLine - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		if strings.HasPrefix(trimmed, prefix) {
			if value == "" {
				// Delete the metadata line
				lines = append(lines[:i], lines[i+1:]...)
			} else {
				lines[i] = fmt.Sprintf("# sshq:%s=%s", key, value)
			}
			return []byte(strings.Join(lines, "\n"))
		}
	}

	if value == "" {
		return raw
	}

	// Not found, insert before Host line (after any existing comments)
	insertAt := hostLine
	newLine := fmt.Sprintf("# sshq:%s=%s", key, value)

	// Check if there's already a comment block, insert at end of it
	for i := hostLine - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "#") {
			insertAt = i + 1
		} else if trimmed == "" {
			continue
		} else {
			break
		}
	}

	// If no comment block exists, create the separator too
	if insertAt == hostLine {
		separator := fmt.Sprintf("# ===== %s =====", alias)
		result := make([]string, 0, len(lines)+2)
		result = append(result, lines[:hostLine]...)
		result = append(result, separator, newLine)
		result = append(result, lines[hostLine:]...)
		return []byte(strings.Join(result, "\n"))
	}

	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:insertAt]...)
	result = append(result, newLine)
	result = append(result, lines[insertAt:]...)
	return []byte(strings.Join(result, "\n"))
}

func canonicalSSHKey(key string) string {
	m := map[string]string{
		"hostname":     "HostName",
		"user":         "User",
		"port":         "Port",
		"identityfile": "IdentityFile",
		"proxyjump":    "ProxyJump",
	}
	if v, ok := m[strings.ToLower(key)]; ok {
		return v
	}
	return key
}
