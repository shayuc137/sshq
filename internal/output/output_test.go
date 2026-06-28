package output

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

// stubRenderable is a test implementation of Renderable.
type stubRenderable struct {
	V string `json:"v"`
}

func (s stubRenderable) Pretty() string { return "pretty:" + s.V }

func prettyWriter() (*Writer, *bytes.Buffer, *bytes.Buffer) {
	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	return New(out, errBuf, WithPretty()), out, errBuf
}

func jsonWriter() (*Writer, *bytes.Buffer, *bytes.Buffer) {
	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	return New(out, errBuf, WithJSON()), out, errBuf
}

// --- Mode resolution ---

func TestNew_PipeDefaultsToJSON(t *testing.T) {
	t.Setenv("SSHQ_OUTPUT", "")
	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	// bytes.Buffer has no Fd(), so isTerminal treats it as a pipe → JSON.
	w := New(out, errBuf)
	w.Success("x")
	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("pipe should default to JSON, got %q: %v", out.String(), err)
	}
	if env["ok"] != true {
		t.Errorf("ok = %v, want true", env["ok"])
	}
}

func TestNew_TerminalDefaultsToPretty(t *testing.T) {
	orig := isTerminal
	isTerminal = func(io.Writer) bool { return true }
	t.Cleanup(func() { isTerminal = orig })
	t.Setenv("SSHQ_OUTPUT", "")

	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	w := New(out, errBuf)
	w.Render(stubRenderable{V: "x"})
	if got := out.String(); got != "pretty:x\n" {
		t.Errorf("terminal should default to pretty, got %q", got)
	}
}

func TestWithJSON_OverridesTerminal(t *testing.T) {
	orig := isTerminal
	isTerminal = func(io.Writer) bool { return true }
	t.Cleanup(func() { isTerminal = orig })

	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	w := New(out, errBuf, WithJSON())
	w.Success("x")
	if !json.Valid(out.Bytes()) {
		t.Errorf("WithJSON should force JSON even on terminal, got %q", out.String())
	}
}

func TestWithPretty_OverridesPipe(t *testing.T) {
	t.Setenv("SSHQ_OUTPUT", "")
	w, out, _ := prettyWriter()
	w.Render(stubRenderable{V: "y"})
	if got := out.String(); got != "pretty:y\n" {
		t.Errorf("WithPretty should force pretty on pipe, got %q", got)
	}
}

func TestModePriority_FlagOverEnv(t *testing.T) {
	t.Setenv("SSHQ_OUTPUT", "json")
	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	w := New(out, errBuf, WithPretty())
	w.Success("done")
	if got := out.String(); got != "done\n" {
		t.Errorf("flag should override env, got %q, want %q", got, "done\n")
	}
}

func TestModePriority_EnvOverTerminal(t *testing.T) {
	orig := isTerminal
	isTerminal = func(io.Writer) bool { return true }
	t.Cleanup(func() { isTerminal = orig })
	t.Setenv("SSHQ_OUTPUT", "json")

	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	w := New(out, errBuf)
	w.Success("x")
	if !json.Valid(out.Bytes()) {
		t.Errorf("env=json should override terminal, got %q", out.String())
	}
}

// --- Render ---

func TestRender_JSONMode(t *testing.T) {
	w, out, _ := jsonWriter()
	w.Render(stubRenderable{V: "hello"})
	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env["ok"] != true {
		t.Errorf("ok = %v, want true", env["ok"])
	}
	data := env["data"].(map[string]any)
	if data["v"] != "hello" {
		t.Errorf("data.v = %v, want hello", data["v"])
	}
	if env["schema_version"].(float64) != 1 {
		t.Errorf("schema_version = %v, want 1", env["schema_version"])
	}
}

func TestRender_PrettyMode(t *testing.T) {
	w, out, _ := prettyWriter()
	w.Render(stubRenderable{V: "hello"})
	if got := out.String(); got != "pretty:hello\n" {
		t.Errorf("Render pretty = %q, want %q", got, "pretty:hello\n")
	}
}

// --- Exec ---

func TestExec_JSONMode(t *testing.T) {
	w, out, _ := jsonWriter()
	w.Exec(&ExecResult{ExitCode: 0, Stdout: "hi\n", Host: "rn", DurationMs: 42})
	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data := env["data"].(map[string]any)
	if data["stdout"] != "hi\n" {
		t.Errorf("data.stdout = %v, want %q", data["stdout"], "hi\n")
	}
	if data["exit_code"].(float64) != 0 {
		t.Errorf("data.exit_code = %v, want 0", data["exit_code"])
	}
	if data["host"] != "rn" {
		t.Errorf("data.host = %v, want rn", data["host"])
	}
	if data["duration_ms"].(float64) != 42 {
		t.Errorf("data.duration_ms = %v, want 42", data["duration_ms"])
	}
}

func TestExec_PrettyMode_MirrorsStreams(t *testing.T) {
	w, out, errBuf := prettyWriter()
	w.Exec(&ExecResult{Stdout: "line1\nline2\n", Stderr: "oops\n"})
	if got := out.String(); got != "line1\nline2\n" {
		t.Errorf("Exec stdout = %q, want %q", got, "line1\nline2\n")
	}
	if got := errBuf.String(); got != "oops\n" {
		t.Errorf("Exec stderr = %q, want %q", got, "oops\n")
	}
}

func TestExec_PrettyMode_NoTrailingNewlineAdded(t *testing.T) {
	w, out, _ := prettyWriter()
	w.Exec(&ExecResult{Stdout: "no-newline"})
	if got := out.String(); got != "no-newline" {
		t.Errorf("Exec must mirror stdout exactly, got %q, want %q", got, "no-newline")
	}
}

// --- Progress ---

func TestProgress_PrettyMode(t *testing.T) {
	w, out, errBuf := prettyWriter()
	w.Progress(ProgressInfo{File: "f.bin", Percent: 50, Transferred: 512, Total: 1024, Speed: "1KB/s"})
	if out.Len() != 0 {
		t.Errorf("Progress must not write to stdout, got %q", out.String())
	}
	want := "f.bin 50% 512B/1.0KB 1KB/s\n"
	if got := errBuf.String(); got != want {
		t.Errorf("Progress = %q, want %q", got, want)
	}
}

func TestProgress_JSONMode(t *testing.T) {
	w, out, errBuf := jsonWriter()
	w.Progress(ProgressInfo{File: "f.bin", Percent: 50, Transferred: 512, Total: 1024, Speed: "1KB/s"})
	if out.Len() != 0 {
		t.Errorf("Progress must not write to stdout, got %q", out.String())
	}
	var info map[string]any
	if err := json.Unmarshal(errBuf.Bytes(), &info); err != nil {
		t.Fatalf("Progress JSON invalid: %v", err)
	}
	if info["file"] != "f.bin" {
		t.Errorf("file = %v, want f.bin", info["file"])
	}
	if info["percent"].(float64) != 50 {
		t.Errorf("percent = %v, want 50", info["percent"])
	}
}

func TestProgress_Disabled(t *testing.T) {
	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	w := New(out, errBuf, WithPretty(), WithNoProgress())
	w.Progress(ProgressInfo{File: "f", Percent: 100, Transferred: 1, Total: 1, Speed: "1B/s"})
	if errBuf.Len() != 0 || out.Len() != 0 {
		t.Errorf("WithNoProgress should suppress all output, got out=%q err=%q", out.String(), errBuf.String())
	}
}

// --- Verbose ---

func TestVerbose_Enabled(t *testing.T) {
	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	w := New(out, errBuf, WithPretty(), WithVerbose())
	w.Verbose("debug msg")
	if out.Len() != 0 {
		t.Errorf("Verbose must not write to stdout, got %q", out.String())
	}
	if got := errBuf.String(); got != "debug msg\n" {
		t.Errorf("Verbose = %q, want %q", got, "debug msg\n")
	}
}

func TestVerbose_Disabled(t *testing.T) {
	w, _, errBuf := prettyWriter()
	w.Verbose("debug msg")
	if errBuf.Len() != 0 {
		t.Errorf("Verbose without WithVerbose should be silent, got %q", errBuf.String())
	}
}

// --- Info ---

func TestInfo_WritesToStderr(t *testing.T) {
	w, out, errBuf := prettyWriter()
	w.Info("connecting...")
	if out.Len() != 0 {
		t.Errorf("Info must not write to stdout, got %q", out.String())
	}
	if got := errBuf.String(); got != "connecting...\n" {
		t.Errorf("Info stderr = %q, want %q", got, "connecting...\n")
	}
}

// --- Success ---

func TestSuccess_PrettyMode(t *testing.T) {
	w, out, _ := prettyWriter()
	w.Success("done")
	if got := out.String(); got != "done\n" {
		t.Errorf("Success = %q, want %q", got, "done\n")
	}
}

func TestSuccess_DefaultMessage(t *testing.T) {
	w, out, _ := prettyWriter()
	w.Success("")
	if got := out.String(); got != "OK\n" {
		t.Errorf("Success(\"\") = %q, want %q", got, "OK\n")
	}
}

func TestSuccess_JSONMode(t *testing.T) {
	w, out, _ := jsonWriter()
	w.Success("started")
	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env["ok"] != true {
		t.Errorf("ok = %v, want true", env["ok"])
	}
	data := env["data"].(map[string]any)
	if data["message"] != "started" {
		t.Errorf("data.message = %v, want started", data["message"])
	}
	if env["schema_version"].(float64) != 1 {
		t.Errorf("schema_version = %v, want 1", env["schema_version"])
	}
}

// --- Error ---

func TestError_PrettyMode(t *testing.T) {
	w, _, errBuf := prettyWriter()
	w.Error(Errorf("not found", "check alias"))
	want := "Error: not found\n  -> check alias\n"
	if got := errBuf.String(); got != want {
		t.Errorf("Error = %q, want %q", got, want)
	}
}

func TestError_PrettyMode_NoAction(t *testing.T) {
	w, _, errBuf := prettyWriter()
	w.Error(Errorf("timeout", ""))
	if got := errBuf.String(); got != "Error: timeout\n" {
		t.Errorf("Error = %q, want %q", got, "Error: timeout\n")
	}
}

func TestError_JSONMode(t *testing.T) {
	w, out, _ := jsonWriter()
	w.Error(Errorf("denied", "run with sudo"))
	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env["ok"] != false {
		t.Errorf("ok = %v, want false", env["ok"])
	}
	errObj := env["error"].(map[string]any)
	if errObj["hint"] != "denied" {
		t.Errorf("error.hint = %v, want denied", errObj["hint"])
	}
	if errObj["action"] != "run with sudo" {
		t.Errorf("error.action = %v, want %q", errObj["action"], "run with sudo")
	}
}

// --- CmdError + env detection ---

func TestCmdError_Format(t *testing.T) {
	e := Errorf("auth failed", "check key permissions")
	want := "auth failed (-> check key permissions)"
	if got := e.Error(); got != want {
		t.Errorf("CmdError.Error() = %q, want %q", got, want)
	}
}

func TestCmdError_NoAction(t *testing.T) {
	e := Errorf("connection refused", "")
	if got := e.Error(); got != "connection refused" {
		t.Errorf("CmdError.Error() = %q, want %q", got, "connection refused")
	}
}

func TestDetectEnvJSONMode(t *testing.T) {
	tests := []struct {
		env  string
		want bool
	}{
		{"json", true},
		{"JSON", true},
		{" json ", true},
		{"text", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Setenv("SSHQ_OUTPUT", tt.env)
		if got := DetectEnvJSONMode(); got != tt.want {
			t.Errorf("DetectEnvJSONMode() with SSHQ_OUTPUT=%q = %v, want %v", tt.env, got, tt.want)
		}
	}
}
