package output

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func newTestWriter() (*Writer, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	return New(out, errBuf), out, errBuf
}

func TestSuccess_TextMode(t *testing.T) {
	w, out, _ := newTestWriter()
	w.Success("done")
	if got := out.String(); got != "done\n" {
		t.Errorf("Success() = %q, want %q", got, "done\n")
	}
}

func TestSuccess_DefaultMessage(t *testing.T) {
	w, out, _ := newTestWriter()
	w.Success("")
	if got := out.String(); got != "OK\n" {
		t.Errorf("Success(\"\") = %q, want %q", got, "OK\n")
	}
}

func TestSuccess_JSONMode(t *testing.T) {
	w, out, _ := newTestWriter()
	w.SetJSONMode(true)
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
		t.Errorf("data.message = %v, want %q", data["message"], "started")
	}
	if env["schema_version"].(float64) != 1 {
		t.Errorf("schema_version = %v, want 1", env["schema_version"])
	}
}

func TestValue_TextMode(t *testing.T) {
	w, out, _ := newTestWriter()
	w.Value("server1 | 192.168.1.1 | online")
	want := "server1 | 192.168.1.1 | online\n"
	if got := out.String(); got != want {
		t.Errorf("Value() = %q, want %q", got, want)
	}
}

func TestValue_JSONMode(t *testing.T) {
	w, out, _ := newTestWriter()
	w.SetJSONMode(true)
	w.Value("result")

	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data := env["data"].(map[string]any)
	if data["value"] != "result" {
		t.Errorf("data.value = %v, want %q", data["value"], "result")
	}
}

func TestInfo_WritesToStderr(t *testing.T) {
	w, out, errBuf := newTestWriter()
	w.Info("connecting...")
	if out.Len() != 0 {
		t.Error("Info() should not write to stdout")
	}
	if got := errBuf.String(); got != "connecting...\n" {
		t.Errorf("Info() stderr = %q, want %q", got, "connecting...\n")
	}
}

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

func TestRenderError_TextMode(t *testing.T) {
	w, _, errBuf := newTestWriter()
	w.RenderError(Errorf("not found", "check alias"))
	want := "Error: not found\n  -> check alias\n"
	if got := errBuf.String(); got != want {
		t.Errorf("RenderError() = %q, want %q", got, want)
	}
}

func TestRenderError_TextMode_NoAction(t *testing.T) {
	w, _, errBuf := newTestWriter()
	w.RenderError(Errorf("timeout", ""))
	want := "Error: timeout\n"
	if got := errBuf.String(); got != want {
		t.Errorf("RenderError() = %q, want %q", got, want)
	}
}

func TestRenderError_JSONMode(t *testing.T) {
	w, out, _ := newTestWriter()
	w.SetJSONMode(true)
	w.RenderError(Errorf("denied", "run with sudo"))

	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env["ok"] != false {
		t.Errorf("ok = %v, want false", env["ok"])
	}
	errObj := env["error"].(map[string]any)
	if errObj["hint"] != "denied" {
		t.Errorf("error.hint = %v, want %q", errObj["hint"], "denied")
	}
	if errObj["action"] != "run with sudo" {
		t.Errorf("error.action = %v, want %q", errObj["action"], "run with sudo")
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
		os.Setenv("SSHQ_OUTPUT", tt.env)
		if got := DetectEnvJSONMode(); got != tt.want {
			t.Errorf("DetectEnvJSONMode() with SSHQ_OUTPUT=%q = %v, want %v", tt.env, got, tt.want)
		}
	}
	os.Unsetenv("SSHQ_OUTPUT")
}

func TestJSONOut(t *testing.T) {
	w, out, _ := newTestWriter()
	w.SetJSONMode(true)
	w.JSONOut(map[string]string{"key": "val"})

	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env["ok"] != true {
		t.Errorf("ok = %v, want true", env["ok"])
	}
	data := env["data"].(map[string]any)
	if data["key"] != "val" {
		t.Errorf("data.key = %v, want %q", data["key"], "val")
	}
}
