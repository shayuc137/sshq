package remote

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const detectPOSIX = `echo "OS=$(uname -s)" && echo "SHELL=$(basename "$SHELL" 2>/dev/null || readlink /proc/$$/exe 2>/dev/null || echo sh)" && echo "HOME=$HOME"`

const detectWindows = `echo "OS=Windows" ; echo "SHELL=powershell" ; echo "HOME=$env:USERPROFILE" ; chcp 2>$null`

func Detect(ctx context.Context, client *ssh.Client) (*Profile, error) {
	p, err := detectPosix(ctx, client)
	if err == nil {
		return p, nil
	}
	return detectWin(ctx, client)
}

func detectPosix(ctx context.Context, client *ssh.Client) (*Profile, error) {
	out, err := runProbe(ctx, client, detectPOSIX)
	if err != nil {
		return nil, err
	}
	return parsePosixOutput(out)
}

func detectWin(ctx context.Context, client *ssh.Client) (*Profile, error) {
	out, err := runProbe(ctx, client, detectWindows)
	if err != nil {
		return nil, fmt.Errorf("windows detect failed: %w", err)
	}
	return parseWindowsOutput(out)
}

func runProbe(ctx context.Context, client *ssh.Client, command string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	done := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		out, err := session.CombinedOutput(command)
		if err != nil {
			errCh <- err
			return
		}
		done <- out
	}()

	select {
	case <-ctx.Done():
		session.Close()
		return "", ctx.Err()
	case err := <-errCh:
		return "", err
	case out := <-done:
		return string(out), nil
	}
}

func parsePosixOutput(out string) (*Profile, error) {
	p := &Profile{DetectedAt: time.Now().Unix()}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "OS":
			p.OS = normOS(v)
		case "SHELL":
			p.Shell = normShell(filepath.Base(v))
		case "HOME":
			p.HomeDir = v
		}
	}
	if p.OS == "" {
		return nil, fmt.Errorf("failed to parse OS from output")
	}
	if p.Shell == "" {
		p.Shell = Sh
	}
	return p, nil
}

func parseWindowsOutput(out string) (*Profile, error) {
	p := &Profile{
		OS:         Windows,
		Shell:      PowerShell,
		DetectedAt: time.Now().Unix(),
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if k, v, ok := strings.Cut(line, "="); ok {
			switch k {
			case "HOME":
				p.HomeDir = v
			}
		}
		if cp := parseCodePage(line); cp != "" {
			p.Encoding = cp
		}
	}
	return p, nil
}

func parseCodePage(line string) string {
	line = strings.TrimSpace(line)
	// chcp output: "Active code page: 936" or just "936"
	// PowerShell chcp may output different formats
	for _, prefix := range []string{"Active code page: ", "活动代码页: "} {
		if after, ok := strings.CutPrefix(line, prefix); ok {
			return codePageToEncoding(strings.TrimSpace(after))
		}
	}
	// bare number on a line by itself
	trimmed := strings.TrimSpace(line)
	if len(trimmed) >= 2 && len(trimmed) <= 5 && isDigits(trimmed) {
		return codePageToEncoding(trimmed)
	}
	return ""
}

func codePageToEncoding(cp string) string {
	switch cp {
	case "65001":
		return ""
	case "936":
		return "gbk"
	case "950":
		return "big5"
	case "932":
		return "shift-jis"
	case "949":
		return "euc-kr"
	default:
		return "cp" + cp
	}
}

func normOS(s string) OS {
	switch strings.ToLower(s) {
	case "linux":
		return Linux
	case "darwin":
		return Darwin
	case "freebsd":
		return FreeBSD
	case "windows", "windows_nt":
		return Windows
	default:
		return Unknown
	}
}

func normShell(s string) Shell {
	switch strings.ToLower(s) {
	case "bash":
		return Bash
	case "ash":
		return Ash
	case "zsh":
		return Zsh
	case "sh", "dash":
		return Sh
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return PowerShell
	case "cmd", "cmd.exe":
		return Cmd
	default:
		return Shell(strings.ToLower(s))
	}
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
