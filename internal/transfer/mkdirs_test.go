package transfer

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSFTPWindowsPathUsesForwardSlashes(t *testing.T) {
	engine := &sftpEngine{windows: true}
	got := engine.normalizeRemotePath(`C:\Users\Admin\Work Files\out.txt`)
	if got != "C:/Users/Admin/Work Files/out.txt" {
		t.Fatalf("normalized path = %q", got)
	}
}

func TestSFTPParentCreationRequiresMkdirs(t *testing.T) {
	stat := func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	mkdirCalls := 0
	mkdirAll := func(path string) error {
		mkdirCalls++
		if path != "C:/Work Files/output" {
			t.Fatalf("mkdir path = %q", path)
		}
		return nil
	}

	err := prepareSFTPRemoteParent("C:/Work Files/output/file.txt", false, stat, mkdirAll)
	var missing *RemoteParentMissingError
	if !errors.As(err, &missing) || mkdirCalls != 0 {
		t.Fatalf("default error = %v, mkdir calls = %d", err, mkdirCalls)
	}

	if err := prepareSFTPRemoteParent("C:/Work Files/output/file.txt", true, stat, mkdirAll); err != nil {
		t.Fatal(err)
	}
	if mkdirCalls != 1 {
		t.Fatalf("mkdir calls = %d, want 1", mkdirCalls)
	}
}

func TestRawWriteCommandMkdirs(t *testing.T) {
	without := rawWriteCommand("/var/lib/app", "/var/lib/app/file.sshq.tmp", false)
	if strings.Contains(without, "mkdir -p") {
		t.Fatalf("default raw command creates directories: %q", without)
	}

	with := rawWriteCommand("/var/lib/app", "/var/lib/app/file.sshq.tmp", true)
	if !strings.Contains(with, "mkdir -p '/var/lib/app'") || !strings.Contains(with, "cat > '/var/lib/app/file.sshq.tmp'") {
		t.Fatalf("mkdirs raw command = %q", with)
	}
}

func TestRemoteParentMissingError(t *testing.T) {
	err := (&RemoteParentMissingError{Path: "/missing/parent"}).Error()
	if !strings.Contains(err, "/missing/parent") || !strings.Contains(err, "does not exist") {
		t.Fatalf("error = %q", err)
	}
}
