package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shayuc137/sshq/internal/appconfig"
)

func TestRecordAndQuery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	logger, err := NewLogger(appconfig.AuditConfig{Path: path, MaxSize: "1MB"})
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer logger.Close()

	if err := logger.Record(ExecEntry("ali", "echo password=secret", ResultSuccess, 0, 12, SourceDirect)); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := Query(path, QueryOpts{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}
	got := entries[0]
	if got.SchemaVersion != SchemaVersion || got.Alias != "ali" || got.Operation != OperationExec {
		t.Fatalf("unexpected entry: %+v", got)
	}
	if got.Result != ResultSuccess || got.DurationMs != 12 || got.Source != SourceDirect {
		t.Fatalf("unexpected result fields: %+v", got)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("exit code = %#v, want 0", got.ExitCode)
	}
	if strings.Contains(got.Summary, "secret") || !strings.Contains(got.Summary, "password=<redacted>") {
		t.Fatalf("summary not redacted: %q", got.Summary)
	}
}

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	logger, err := NewLogger(appconfig.AuditConfig{Path: path, MaxSize: "1B"})
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer logger.Close()

	for i := 0; i < 3; i++ {
		if err := logger.Record(ExecEntry("ali", strings.Repeat("x", 50), ResultSuccess, 0, int64(i), SourceDirect)); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}

	rotated, err := filepath.Glob(filepath.Join(dir, "audit-*.log"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(rotated) == 0 {
		t.Fatalf("expected rotated audit logs")
	}

	entries, err := Query(path, QueryOpts{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries len = %d, want 3", len(entries))
	}
}

func TestQueryFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	logger, err := NewLogger(appconfig.AuditConfig{Path: path, MaxSize: "1MB"})
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer logger.Close()

	base := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	entries := []Entry{
		{Timestamp: base.Add(1 * time.Second).Format(time.RFC3339Nano), Alias: "ali", Operation: OperationExec, Summary: "one", Result: ResultSuccess, Source: SourceDirect},
		{Timestamp: base.Add(2 * time.Second).Format(time.RFC3339Nano), Alias: "rn", Operation: OperationCP, Summary: "two", Result: ResultSuccess, Source: SourceDirect},
		{Timestamp: base.Add(3 * time.Second).Format(time.RFC3339Nano), Alias: "ali", Operation: OperationCP, Summary: "three", Result: ResultSuccess, Source: SourceDirect},
	}
	for _, entry := range entries {
		if err := logger.Record(entry); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	byAlias, err := Query(path, QueryOpts{Alias: "ali"})
	if err != nil {
		t.Fatalf("Query alias: %v", err)
	}
	if len(byAlias) != 2 {
		t.Fatalf("alias filter len = %d, want 2", len(byAlias))
	}

	byOperation, err := Query(path, QueryOpts{Operation: OperationCP})
	if err != nil {
		t.Fatalf("Query operation: %v", err)
	}
	if len(byOperation) != 2 {
		t.Fatalf("operation filter len = %d, want 2", len(byOperation))
	}

	last, err := Query(path, QueryOpts{Last: 1})
	if err != nil {
		t.Fatalf("Query last: %v", err)
	}
	if len(last) != 1 || last[0].Summary != "three" {
		t.Fatalf("last = %+v, want newest entry", last)
	}
}

func TestRedactSummary(t *testing.T) {
	got := RedactSummary("run --password secret " + strings.Repeat("x", 250))
	if strings.Contains(got, "secret") {
		t.Fatalf("summary leaked secret: %q", got)
	}
	if count := len([]rune(got)); count > SummaryLimit {
		t.Fatalf("summary rune count = %d, want <= %d", count, SummaryLimit)
	}
}

func TestScriptSummary(t *testing.T) {
	script := []byte("echo token=abc\nsecond line")
	got := ScriptSummary(script)
	sum := sha256.Sum256(script)
	wantHash := hex.EncodeToString(sum[:])
	if !strings.Contains(got, "sha256="+wantHash) {
		t.Fatalf("summary missing hash: %q", got)
	}
	if !strings.Contains(got, "bytes=26") {
		t.Fatalf("summary missing byte count: %q", got)
	}
	if strings.Contains(got, "abc") || strings.Contains(got, "second line") {
		t.Fatalf("summary leaked script content: %q", got)
	}
	if !strings.Contains(got, "first_line=echo token=<redacted>") {
		t.Fatalf("summary missing redacted first line: %q", got)
	}
}

func TestCorruptLineSkip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	entry := ExecEntry("ali", "hostname", ResultSuccess, 0, 1, SourceDirect)
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, []byte("{bad json\n"+string(raw)+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	warnings := 0
	entries, err := Query(path, QueryOpts{Warn: func(string) { warnings++ }})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}
	if warnings != 1 {
		t.Fatalf("warnings = %d, want 1", warnings)
	}
}

func TestFilePermission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows ACLs are not represented by chmod bits")
	}
	path := filepath.Join(t.TempDir(), "audit.log")
	logger, err := NewLogger(appconfig.AuditConfig{Path: path, MaxSize: "1MB"})
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer logger.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("permission = %04o, want 0600", perm)
	}
}

func TestEmptyAudit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "audit.log")
	entries, err := Query(path, QueryOpts{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if entries == nil {
		t.Fatalf("entries is nil, want empty slice")
	}
	if len(entries) != 0 {
		t.Fatalf("entries len = %d, want 0", len(entries))
	}
}
