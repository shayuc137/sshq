package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigAddPrettyRendersResolvedHostAndDoctor(t *testing.T) {
	cmd, out, _ := rootCommandForTest(t)
	cmd.SetArgs([]string{
		"--pretty", "config", "add", "win",
		"--hostname", "192.0.2.20",
		"--user", "administrator",
		"--port", "2222",
		"--identity", "~/.ssh/win key",
		"--proxy-jump", "jump",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	for _, want := range []string{
		"Alias:        win",
		"HostName:     192.0.2.20",
		"User:         administrator",
		"Port:         2222",
		"IdentityFile: ~/.ssh/win key",
		"ProxyJump:    jump",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("pretty output missing %q:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "next: sshq doctor win") {
		t.Fatalf("pretty output missing final doctor hint:\n%s", got)
	}
}

func TestConfigSetJSONRendersResolvedHostAndDoctor(t *testing.T) {
	cmd, out, _ := rootCommandForTest(t, "win")
	cmd.SetArgs([]string{"--json", "config", "set", "win", "proxyjump", "jump"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Alias        string `json:"alias"`
			HostName     string `json:"hostname"`
			User         string `json:"user"`
			Port         string `json:"port"`
			IdentityFile string `json:"identity_file"`
			ProxyJump    string `json:"proxy_jump"`
			Next         string `json:"next"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if !envelope.OK || envelope.Data.Alias != "win" || envelope.Data.HostName != "192.0.2.10" || envelope.Data.User != "tester" || envelope.Data.Port != "22" {
		t.Fatalf("resolved data = %+v", envelope.Data)
	}
	if envelope.Data.ProxyJump != "jump" || envelope.Data.Next != "sshq doctor win" {
		t.Fatalf("verification data = %+v", envelope.Data)
	}
	var raw struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"alias", "hostname", "user", "port", "identity_file", "proxy_jump", "next"} {
		if _, ok := raw.Data[field]; !ok {
			t.Fatalf("JSON data missing fixed field %q: %s", field, out.String())
		}
	}
}
