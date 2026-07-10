package exec

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/shayuc137/sshq/internal/remote"
)

func TestNormalizeShell(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"PowerShell", "powershell"},
		{"powershell.exe", "powershell"},
		{"pwsh", "powershell"},
		{"pwsh.exe", "powershell"},
		{"cmd.exe", "cmd"},
		{" bash ", "bash"},
	}

	for _, tt := range tests {
		if got := normalizeShell(tt.in); got != tt.want {
			t.Errorf("normalizeShell(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestInterpreterCmd_NormalizesPowerShell(t *testing.T) {
	got, err := InterpreterCmd("pwsh.exe", []byte("Write-Output 'ok'"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, powerShellPrefix+" -EncodedCommand ") {
		t.Fatalf("InterpreterCmd(pwsh.exe) = %q", got)
	}
}

func TestPowerShellEncodedCommandUTF16LE(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{name: "chinese", script: `Write-Output "中文输出"`},
		{name: "here string", script: "$value = @\"\nline one\nline two\n\"@\nWrite-Output $value"},
		{name: "multiline block", script: "$items = @(\n  'one'\n  'two'\n)\n$items | ForEach-Object { Write-Output $_ }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := powerShellEncodedCommand([]byte(tt.script))
			encoded := strings.TrimPrefix(command, powerShellPrefix+" -EncodedCommand ")
			if encoded == command {
				t.Fatalf("command missing expected flags: %q", command)
			}
			raw, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatalf("decode base64: %v", err)
			}
			if len(raw)%2 != 0 {
				t.Fatalf("UTF-16LE payload has odd byte count: %d", len(raw))
			}
			codeUnits := make([]uint16, len(raw)/2)
			for i := range codeUnits {
				codeUnits[i] = binary.LittleEndian.Uint16(raw[i*2:])
			}
			// The encoder prepends a same-line progress-suppression statement
			// so PowerShell 5.1 does not emit CLIXML progress noise on stderr.
			want := "$ProgressPreference='SilentlyContinue'; " + tt.script
			if got := string(utf16.Decode(codeUnits)); got != want {
				t.Fatalf("decoded script = %q, want %q", got, want)
			}
		})
	}
}

func TestRunPowerShellScriptBufferedThreshold(t *testing.T) {
	profile := &remote.Profile{OS: remote.Windows, TempDir: `C:\Users\tester\AppData\Local\Temp`}
	tests := []struct {
		name       string
		size       int
		wantUpload bool
	}{
		{name: "at inline limit", size: powerShellInlineLimit},
		{name: "above inline limit", size: powerShellInlineLimit + 1, wantUpload: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops := &fakePowerShellScriptOps{result: &Result{ExitCode: 0, Stdout: "ok\n"}}
			var verbose []string
			result, err := RunScriptBuffered(context.Background(), nil, []byte(strings.Repeat("x", tt.size)), "powershell",
				WithRemoteProfile(profile),
				WithScriptVerbose(func(msg string) { verbose = append(verbose, msg) }),
				withPowerShellScriptOps(ops),
			)
			if err != nil {
				t.Fatal(err)
			}
			if result.Stdout != "ok\n" {
				t.Fatalf("result = %+v", result)
			}
			if got := len(ops.uploads) > 0; got != tt.wantUpload {
				t.Fatalf("upload=%v, want %v", got, tt.wantUpload)
			}
			if !tt.wantUpload {
				if len(ops.removes) != 0 || len(verbose) != 0 {
					t.Fatalf("inline side effects: removes=%v verbose=%v", ops.removes, verbose)
				}
				if !strings.Contains(ops.commands[0], " -EncodedCommand ") {
					t.Fatalf("inline command = %q", ops.commands[0])
				}
				return
			}

			if len(ops.removes) != 1 || ops.removes[0] != ops.uploads[0].remotePath {
				t.Fatalf("cleanup=%v upload=%+v", ops.removes, ops.uploads)
			}
			if len(verbose) != 1 || verbose[0] != "script exceeds inline limit, using upload-run" {
				t.Fatalf("verbose=%v", verbose)
			}
			if got := ops.uploads[0].content[:3]; string(got) != string([]byte{0xef, 0xbb, 0xbf}) {
				t.Fatalf("upload prefix = %x, want UTF-8 BOM", got)
			}
			if got := ops.uploads[0].content[3:]; string(got) != strings.Repeat("x", tt.size) {
				t.Fatalf("uploaded script content changed")
			}
			if !strings.Contains(ops.commands[0], ` -File "`) || !strings.Contains(ops.commands[0], "/sshq-script-") {
				t.Fatalf("upload-run command = %q", ops.commands[0])
			}
			if _, err := os.Stat(ops.uploads[0].localPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("local temp file still exists: %s", ops.uploads[0].localPath)
			}
		})
	}
}

func TestRunPowerShellScriptBufferedCleanupFailureIsVerboseOnly(t *testing.T) {
	ops := &fakePowerShellScriptOps{
		result:    &Result{ExitCode: 7, Stderr: "script failed"},
		removeErr: errors.New("access denied"),
	}
	var verbose []string
	result, err := RunScriptBuffered(context.Background(), nil, []byte(strings.Repeat("x", powerShellInlineLimit+1)), "powershell",
		WithRemoteProfile(&remote.Profile{OS: remote.Windows, TempDir: `C:\Temp`}),
		WithScriptVerbose(func(msg string) { verbose = append(verbose, msg) }),
		withPowerShellScriptOps(ops),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", result.ExitCode)
	}
	if len(verbose) != 2 || !strings.Contains(verbose[1], "remote script cleanup failed: access denied") {
		t.Fatalf("verbose = %v", verbose)
	}
}

func TestPowerShellTempPathRandomized(t *testing.T) {
	profile := &remote.Profile{HomeDir: `C:\Users\tester`}
	first, err := powerShellTempPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := powerShellTempPath(profile)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("temp paths are identical: %q", first)
	}
	if !strings.HasPrefix(first, "C:/Users/tester/AppData/Local/Temp/sshq-script-") || !strings.HasSuffix(first, ".ps1") {
		t.Fatalf("temp path = %q", first)
	}
}

type fakePowerShellUpload struct {
	localPath  string
	remotePath string
	content    []byte
}

type fakePowerShellScriptOps struct {
	commands  []string
	uploads   []fakePowerShellUpload
	removes   []string
	result    *Result
	runErr    error
	removeErr error
}

func (f *fakePowerShellScriptOps) Run(_ context.Context, command string) (*Result, error) {
	f.commands = append(f.commands, command)
	return f.result, f.runErr
}

func (f *fakePowerShellScriptOps) Upload(_ context.Context, localPath, remotePath string) error {
	content, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	f.uploads = append(f.uploads, fakePowerShellUpload{localPath: localPath, remotePath: remotePath, content: content})
	return nil
}

func (f *fakePowerShellScriptOps) Remove(_ context.Context, remotePath string) error {
	f.removes = append(f.removes, remotePath)
	return f.removeErr
}
