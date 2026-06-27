package sshclient

import "testing"

func TestResolveProxyConfig(t *testing.T) {
	tests := []struct {
		input    string
		wantHost string
		wantPort string
		wantUser string
	}{
		{"jumphost", "jumphost", "22", ""},
		{"user@jumphost", "jumphost", "22", "user"},
		{"user@jumphost:2222", "jumphost", "2222", "user"},
		{"jumphost:2222", "jumphost", "2222", ""},
		{"10.0.0.1", "10.0.0.1", "22", ""},
		{"admin@10.0.0.1:9527", "10.0.0.1", "9527", "admin"},
	}
	for _, tt := range tests {
		cfg, err := resolveProxyConfig(tt.input)
		if err != nil {
			t.Errorf("resolveProxyConfig(%q) error: %v", tt.input, err)
			continue
		}
		if cfg.Host != tt.wantHost {
			t.Errorf("resolveProxyConfig(%q).Host = %q, want %q", tt.input, cfg.Host, tt.wantHost)
		}
		if cfg.Port != tt.wantPort {
			t.Errorf("resolveProxyConfig(%q).Port = %q, want %q", tt.input, cfg.Port, tt.wantPort)
		}
		if cfg.User != tt.wantUser {
			t.Errorf("resolveProxyConfig(%q).User = %q, want %q", tt.input, cfg.User, tt.wantUser)
		}
	}
}

func TestResolveProxyConfigEmpty(t *testing.T) {
	_, err := resolveProxyConfig("")
	if err == nil {
		t.Error("expected error for empty proxy")
	}
}
