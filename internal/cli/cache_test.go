package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/remote"
)

func TestCacheClearAliasRendersJSONEnvelope(t *testing.T) {
	store := configStoreForDoctorTest(t, t.TempDir())
	cache := cacheForCLITest(t)
	cache.Put("192.0.2.10", "22", cachedTestProfile())
	out := &bytes.Buffer{}

	cmd := newCacheClearCommand()
	ctx := withConfig(context.Background(), store)
	ctx = withProfileCache(ctx, cache)
	ctx = withWriter(ctx, output.New(out, &bytes.Buffer{}, output.WithJSON()))
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"target"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertCacheClearEnvelope(t, out.Bytes(), 1)
	if _, ok := cache.Get("192.0.2.10", "22"); ok {
		t.Fatal("target profile remains cached")
	}
}

func TestCacheClearUnknownAliasReturnsStandardErrorEnvelope(t *testing.T) {
	store := configStoreForDoctorTest(t, t.TempDir())
	cache := cacheForCLITest(t)
	out := &bytes.Buffer{}
	w := output.New(out, &bytes.Buffer{}, output.WithJSON())

	cmd := newCacheClearCommand()
	ctx := withConfig(context.Background(), store)
	ctx = withProfileCache(ctx, cache)
	ctx = withWriter(ctx, w)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"missing"})

	err := cmd.Execute()
	var cmdErr *output.CmdError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error = %v, want output.CmdError", err)
	}
	w.Error(cmdErr)

	var envelope struct {
		Error struct {
			Code   string `json:"code"`
			Hint   string `json:"hint"`
			Action string `json:"action"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if envelope.Error.Code != "internal_error" {
		t.Fatalf("error code = %q, want internal_error", envelope.Error.Code)
	}
	if envelope.Error.Hint != `host "missing" not found` || envelope.Error.Action != "run 'sshq ls' to see available hosts" {
		t.Fatalf("error = %+v", envelope.Error)
	}
}

func TestCacheClearAllReportsExactCount(t *testing.T) {
	cache := cacheForCLITest(t)
	cache.Put("one.example", "22", cachedTestProfile())
	cache.Put("two.example", "2222", cachedTestProfile())
	out := &bytes.Buffer{}

	cmd := newCacheClearCommand()
	ctx := withProfileCache(context.Background(), cache)
	ctx = withWriter(ctx, output.New(out, &bytes.Buffer{}, output.WithJSON()))
	cmd.SetContext(ctx)

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertCacheClearEnvelope(t, out.Bytes(), 2)
	if got := cache.Clear(); got != 0 {
		t.Fatalf("second clear count = %d, want 0", got)
	}
}

func TestCacheCommandOnlyRegistersClear(t *testing.T) {
	cmd := newCacheCommand()
	if len(cmd.Commands()) != 1 || cmd.Commands()[0].Name() != "clear" {
		t.Fatalf("subcommands = %v, want only clear", cmd.Commands())
	}
}

func cacheForCLITest(t *testing.T) *remote.Cache {
	t.Helper()
	cache, err := remote.NewCache(time.Hour, remote.WithCachePath(filepath.Join(t.TempDir(), "profiles.json")))
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

func cachedTestProfile() *remote.Profile {
	return &remote.Profile{OS: remote.Linux, Shell: remote.Bash, DetectedAt: time.Now().Unix()}
}

func assertCacheClearEnvelope(t *testing.T, data []byte, wantCleared int) {
	t.Helper()
	var envelope struct {
		Data struct {
			Cleared int `json:"cleared"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	if envelope.Data.Cleared != wantCleared {
		t.Fatalf("envelope = %+v, want cleared=%d", envelope, wantCleared)
	}
}
