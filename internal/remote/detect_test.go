package remote

import "testing"

func TestParsePosixOutput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantOS  OS
		wantSh  Shell
		wantErr bool
	}{
		{
			name:   "linux bash",
			input:  "OS=Linux\nSHELL=bash\nHOME=/root\n",
			wantOS: Linux,
			wantSh: Bash,
		},
		{
			name:   "linux ash busybox",
			input:  "OS=Linux\nSHELL=ash\nHOME=/root\n",
			wantOS: Linux,
			wantSh: Ash,
		},
		{
			name:   "darwin zsh",
			input:  "OS=Darwin\nSHELL=zsh\nHOME=/Users/shayu\n",
			wantOS: Darwin,
			wantSh: Zsh,
		},
		{
			name:   "freebsd sh",
			input:  "OS=FreeBSD\nSHELL=sh\nHOME=/home/user\n",
			wantOS: FreeBSD,
			wantSh: Sh,
		},
		{
			name:   "shell from proc readlink",
			input:  "OS=Linux\nSHELL=/bin/bash\nHOME=/root\n",
			wantOS: Linux,
			wantSh: Bash,
		},
		{
			name:   "missing shell defaults to sh",
			input:  "OS=Linux\nHOME=/root\n",
			wantOS: Linux,
			wantSh: Sh,
		},
		{
			name:    "unknown OS should fallback",
			input:   "OS=\nSHELL=sh\nHOME=C:\\Users\\szdos\n",
			wantErr: true,
		},
		{
			name:    "empty output",
			input:   "",
			wantErr: true,
		},
		{
			name:    "garbage",
			input:   "random text\nno key value\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := parsePosixOutput(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.OS != tt.wantOS {
				t.Errorf("OS = %q, want %q", p.OS, tt.wantOS)
			}
			if p.Shell != tt.wantSh {
				t.Errorf("Shell = %q, want %q", p.Shell, tt.wantSh)
			}
		})
	}
}

func TestParseWindowsOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantEnc  string
		wantHome string
	}{
		{
			name:     "powershell with gbk",
			input:    "OS=Windows\nSHELL=powershell\nHOME=C:\\Users\\admin\nActive code page: 936\n",
			wantEnc:  "gbk",
			wantHome: "C:\\Users\\admin",
		},
		{
			name:    "powershell utf8",
			input:   "OS=Windows\nSHELL=powershell\nHOME=C:\\Users\\admin\nActive code page: 65001\n",
			wantEnc: "",
		},
		{
			name:    "chinese chcp",
			input:   "OS=Windows\n活动代码页: 936\n",
			wantEnc: "gbk",
		},
		{
			name:    "no chcp output",
			input:   "OS=Windows\nSHELL=powershell\n",
			wantEnc: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := parseWindowsOutput(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Encoding != tt.wantEnc {
				t.Errorf("Encoding = %q, want %q", p.Encoding, tt.wantEnc)
			}
			if tt.wantHome != "" && p.HomeDir != tt.wantHome {
				t.Errorf("HomeDir = %q, want %q", p.HomeDir, tt.wantHome)
			}
			if p.OS != Windows {
				t.Errorf("OS = %q, want windows", p.OS)
			}
		})
	}
}

func TestParseCodePage(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"Active code page: 936", "gbk"},
		{"Active code page: 65001", ""},
		{"Active code page: 950", "big5"},
		{"Active code page: 932", "shift-jis"},
		{"Active code page: 949", "euc-kr"},
		{"活动代码页: 936", "gbk"},
		{"\xbb\xee\xb6\xaf\xb4\xfa\xc2\xeb\xd2\xb3: 936", "gbk"},
		{"random text", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got := parseCodePage(tt.line)
			if got != tt.want {
				t.Errorf("parseCodePage(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestNormOS(t *testing.T) {
	tests := []struct{ in string; want OS }{
		{"Linux", Linux},
		{"linux", Linux},
		{"Darwin", Darwin},
		{"FreeBSD", FreeBSD},
		{"Windows_NT", Windows},
		{"something", Unknown},
	}
	for _, tt := range tests {
		if got := normOS(tt.in); got != tt.want {
			t.Errorf("normOS(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormShell(t *testing.T) {
	tests := []struct{ in string; want Shell }{
		{"bash", Bash},
		{"ash", Ash},
		{"zsh", Zsh},
		{"sh", Sh},
		{"dash", Sh},
		{"fish", Fish},
		{"ksh", Ksh},
		{"ksh93", Ksh},
		{"mksh", Ksh},
		{"tcsh", Tcsh},
		{"csh", Csh},
		{"powershell.exe", PowerShell},
		{"pwsh", PowerShell},
		{"cmd.exe", Cmd},
		{"nushell", "nushell"},
	}
	for _, tt := range tests {
		if got := normShell(tt.in); got != tt.want {
			t.Errorf("normShell(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestProfileMethods(t *testing.T) {
	posix := &Profile{OS: Linux, Shell: Bash}
	if !posix.IsPOSIX() {
		t.Error("Linux should be POSIX")
	}
	if posix.IsWindows() {
		t.Error("Linux should not be Windows")
	}
	if posix.InterpreterCmd() != "bash -s" {
		t.Errorf("InterpreterCmd = %q, want 'bash -s'", posix.InterpreterCmd())
	}
	if posix.NeedsStdinInjection() {
		t.Error("bash should not need stdin injection")
	}

	win := &Profile{OS: Windows, Shell: PowerShell}
	if win.IsPOSIX() {
		t.Error("Windows should not be POSIX")
	}
	if !win.IsWindows() {
		t.Error("Windows should be Windows")
	}
	if win.InterpreterCmd() != "powershell -NoProfile -NonInteractive -Command -" {
		t.Errorf("InterpreterCmd = %q", win.InterpreterCmd())
	}
	if !win.NeedsStdinInjection() {
		t.Error("powershell should need stdin injection")
	}

	cmd := &Profile{OS: Windows, Shell: Cmd}
	if !cmd.NeedsStdinInjection() {
		t.Error("cmd should need stdin injection")
	}

	fish := &Profile{OS: Linux, Shell: Fish}
	if fish.NeedsStdinInjection() {
		t.Error("fish should not need stdin injection")
	}
}
