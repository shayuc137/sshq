package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shayuc137/sshq/internal/appconfig"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/policy"
)

func TestPolicyCheckDeniedRendersDecisionAndReturnsBadNews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[policy.default]\ncommand_whitelist = [\"^uptime$\"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := appconfig.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	checker := policy.NewChecker(cfg, nil)
	var out bytes.Buffer
	ctx := withPolicyChecker(context.Background(), checker)
	ctx = withWriter(ctx, output.New(&out, &bytes.Buffer{}, output.WithJSON()))

	cmd := newPolicyCheckCommand()
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"target", "--command", "rm -rf /tmp/example"})
	err = cmd.Execute()
	assertBadNews(t, err)

	var envelope struct {
		ExitCode *int `json:"exit_code"`
		Data     struct {
			Decision policy.Decision `json:"decision"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if envelope.ExitCode != nil || envelope.Data.Decision.Allowed {
		t.Fatalf("policy envelope = %+v", envelope)
	}
}
