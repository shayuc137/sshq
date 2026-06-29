package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/shayuc137/sshq/internal/appconfig"
	"github.com/shayuc137/sshq/internal/credential"
)

const (
	SchemaVersion  = 1
	DefaultMaxSize = 10 * 1000 * 1000

	OperationExec        = "exec"
	OperationCP          = "cp"
	OperationClusterExec = "cluster-exec"
	OperationTunnelStart = "tunnel-start"
	OperationTunnelStop  = "tunnel-stop"

	ResultSuccess = "success"
	ResultError   = "error"
	ResultBlocked = "blocked"

	SourceDirect = "direct"
	SourceDaemon = "daemon"
)

type Entry struct {
	SchemaVersion  int      `json:"schema_version"`
	Timestamp      string   `json:"timestamp"`
	Alias          string   `json:"alias"`
	Aliases        []string `json:"aliases,omitempty"`
	Operation      string   `json:"operation"`
	Summary        string   `json:"summary"`
	Result         string   `json:"result"`
	DurationMs     int64    `json:"duration_ms"`
	Source         string   `json:"source"`
	ExitCode       *int     `json:"exit_code"`
	RequestID      string   `json:"request_id,omitempty"`
	ErrorHint      string   `json:"error_hint,omitempty"`
	BlockedBy      string   `json:"blocked_by,omitempty"`
	MatchedPattern string   `json:"matched_pattern,omitempty"`
	GrantID        string   `json:"grant_id,omitempty"`
}

type Logger struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	file    *os.File
}

func DefaultPath() (string, error) {
	dir, err := credential.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "audit.log"), nil
}

func Enabled(cfg *appconfig.Config) bool {
	return cfg != nil && cfg.Audit.Enabled != nil && *cfg.Audit.Enabled
}

func NewLogger(cfg appconfig.AuditConfig) (*Logger, error) {
	path := cfg.Path
	if path == "" {
		defaultPath, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		path = defaultPath
	}

	maxSize := int64(DefaultMaxSize)
	if cfg.MaxSize != "" {
		parsed, err := appconfig.ParseSize(cfg.MaxSize)
		if err != nil {
			return nil, fmt.Errorf("parse audit max_size: %w", err)
		}
		if parsed <= 0 {
			return nil, fmt.Errorf("audit max_size must be positive")
		}
		maxSize = parsed
	}

	if err := ensureSecureDir(filepath.Dir(path)); err != nil {
		return nil, err
	}

	f, err := openSecureFile(path)
	if err != nil {
		return nil, err
	}
	return &Logger{path: path, maxSize: maxSize, file: f}, nil
}

func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *Logger) Record(e Entry) error {
	if l == nil {
		return nil
	}
	normalizeEntry(&e)

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal audit entry: %w", err)
	}
	data = append(data, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		return fmt.Errorf("audit logger closed")
	}
	if err := l.rotateIfNeededLocked(int64(len(data))); err != nil {
		return err
	}
	n, err := l.file.Write(data)
	if err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("write audit log: %w", io.ErrShortWrite)
	}
	return nil
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	if err != nil {
		return fmt.Errorf("close audit log: %w", err)
	}
	return nil
}

func normalizeEntry(e *Entry) {
	if e.SchemaVersion == 0 {
		e.SchemaVersion = SchemaVersion
	}
	if e.Timestamp == "" {
		e.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	e.Summary = RedactSummary(e.Summary)
}

func ExecEntry(alias, command, result string, exitCode int, durationMs int64, source string) Entry {
	code := exitCode
	return baseEntry(OperationExec, alias, ExecSummary(command), result, durationMs, source, &code)
}

func ExecErrorEntry(alias, command, result string, durationMs int64, source string, err error) Entry {
	e := baseEntry(OperationExec, alias, ExecSummary(command), result, durationMs, source, nil)
	e.ErrorHint = errorHint(err)
	return e
}

func ScriptEntry(alias string, script []byte, result string, exitCode int, durationMs int64, source string) Entry {
	code := exitCode
	return baseEntry(OperationExec, alias, ScriptSummary(script), result, durationMs, source, &code)
}

func ScriptErrorEntry(alias string, script []byte, result string, durationMs int64, source string, err error) Entry {
	e := baseEntry(OperationExec, alias, ScriptSummary(script), result, durationMs, source, nil)
	e.ErrorHint = errorHint(err)
	return e
}

func TransferEntry(alias, direction, localPath, remotePath, result string, durationMs int64, source string) Entry {
	return baseEntry(OperationCP, alias, TransferSummary(direction, localPath, remotePath), result, durationMs, source, nil)
}

func RelayEntry(srcAlias, srcPath, dstAlias, dstPath, result string, durationMs int64, source string) Entry {
	e := baseEntry(OperationCP, srcAlias, RelaySummary(srcAlias, srcPath, dstAlias, dstPath), result, durationMs, source, nil)
	e.Aliases = []string{srcAlias, dstAlias}
	return e
}

func ClusterEntry(aliases []string, command, result string, durationMs int64, source string) Entry {
	e := baseEntry(OperationClusterExec, "", ExecSummary(command), result, durationMs, source, nil)
	e.Aliases = append([]string(nil), aliases...)
	return e
}

func TunnelEntry(alias, direction, localAddr, remoteAddr, action, result string, durationMs int64, source string) Entry {
	operation := OperationTunnelStart
	if action == "stop" {
		operation = OperationTunnelStop
	}
	return baseEntry(operation, alias, TunnelSummary(direction, localAddr, remoteAddr, action), result, durationMs, source, nil)
}

func BlockedEntry(alias, operation, summary, blockedBy, matchedPattern string, source string) Entry {
	e := baseEntry(operation, alias, summary, ResultBlocked, 0, source, nil)
	e.BlockedBy = blockedBy
	e.MatchedPattern = matchedPattern
	return e
}

func baseEntry(operation, alias, summary, result string, durationMs int64, source string, exitCode *int) Entry {
	return Entry{
		SchemaVersion: SchemaVersion,
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
		Alias:         alias,
		Operation:     operation,
		Summary:       RedactSummary(summary),
		Result:        result,
		DurationMs:    durationMs,
		Source:        source,
		ExitCode:      exitCode,
	}
}

func errorHint(err error) string {
	if err == nil {
		return ""
	}
	return RedactSummary(err.Error())
}

func ensureSecureDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create audit directory: %w", err)
	}
	if err := setDirPermission(dir, 0700); err != nil {
		return err
	}
	return nil
}

func openSecureFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	if err := setFilePermission(path, 0600); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
