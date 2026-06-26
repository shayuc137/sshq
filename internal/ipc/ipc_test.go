package ipc

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
)

func TestSendRecv(t *testing.T) {
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

	req := Request{Action: "exec", Alias: "test", Command: "hostname", ProtocolVersion: 1}
	if err := Send(conn, req); err != nil {
		t.Fatal(err)
	}

	msg := <-done
	var got Request
	if err := json.Unmarshal(msg, &got); err != nil {
		t.Fatal(err)
	}
	if got.Action != "exec" || got.Alias != "test" || got.Command != "hostname" {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
}

func TestSendRecv_Frame(t *testing.T) {
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
