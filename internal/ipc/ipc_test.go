package ipc

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
)

func TestSendRecvEnvelope(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan json.RawMessage, 1)
	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		msg, _ := Recv(conn)
		done <- msg
	}()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	env, err := MakeEnvelope("exec", ExecPayload{Alias: "test", Command: "hostname", Timeout: 30})
	if err != nil {
		t.Fatal(err)
	}
	if err := Send(conn, env); err != nil {
		t.Fatal(err)
	}

	msg := <-done
	got, err := ParseEnvelope(msg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != "exec" || got.Version != ProtocolVersion {
		t.Errorf("envelope mismatch: %+v", got)
	}

	var payload ExecPayload
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Alias != "test" || payload.Command != "hostname" {
		t.Errorf("payload mismatch: %+v", payload)
	}
}

func TestSendRecvFrame(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		Send(conn, Frame{Type: "stdout", Data: "hello\n"})
		Send(conn, Frame{Type: "stderr", Data: "warn\n"})
		Send(conn, Frame{Type: "exit", Code: 0})
	}()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var frames []Frame
	for {
		msg, err := Recv(conn)
		if err != nil {
			break
		}
		var f Frame
		json.Unmarshal(msg, &f)
		frames = append(frames, f)
		if f.Type == "exit" || f.Type == "error" {
			break
		}
	}

	if len(frames) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(frames))
	}
	if frames[0].Type != "stdout" || frames[0].Data != "hello\n" {
		t.Errorf("frame[0] = %+v", frames[0])
	}
	if frames[1].Type != "stderr" || frames[1].Data != "warn\n" {
		t.Errorf("frame[1] = %+v", frames[1])
	}
	if frames[2].Type != "exit" || frames[2].Code != 0 {
		t.Errorf("frame[2] = %+v", frames[2])
	}
}

func TestDetectV1(t *testing.T) {
	v1 := json.RawMessage(`{"action":"exec","protocol_version":1,"alias":"test","command":"ls"}`)
	if !DetectV1(v1) {
		t.Error("should detect v1 request")
	}

	v2 := json.RawMessage(`{"action":"exec","v":2,"payload":{}}`)
	if DetectV1(v2) {
		t.Error("should not detect v2 as v1")
	}

	garbage := json.RawMessage(`not json`)
	if DetectV1(garbage) {
		t.Error("garbage should not be detected as v1")
	}
}

func TestParseEnvelopeMissingAction(t *testing.T) {
	raw := json.RawMessage(`{"v":2}`)
	_, err := ParseEnvelope(raw)
	if err == nil {
		t.Error("expected error for missing action")
	}
}

func TestMakeEnvelopeNilPayload(t *testing.T) {
	env, err := MakeEnvelope("shutdown", nil)
	if err != nil {
		t.Fatal(err)
	}
	if env.Action != "shutdown" || env.Payload != nil {
		t.Errorf("unexpected: %+v", env)
	}
}

func TestMakeResultFrame(t *testing.T) {
	result := ProfileResult{OS: "linux", Shell: "bash", Encoding: "utf-8"}
	frame, err := MakeResultFrame(result)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Type != "result" {
		t.Errorf("expected type=result, got %s", frame.Type)
	}

	var got ProfileResult
	if err := json.Unmarshal(frame.Payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.OS != "linux" || got.Shell != "bash" {
		t.Errorf("result mismatch: %+v", got)
	}
}

func TestFrameBackwardCompat(t *testing.T) {
	f := Frame{Type: "stdout", Data: "hello"}
	b, _ := json.Marshal(f)

	var parsed Frame
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Data != "hello" {
		t.Errorf("Data field lost: %+v", parsed)
	}
	if parsed.Payload != nil {
		t.Errorf("Payload should be nil for legacy frames: %s", parsed.Payload)
	}
}
