package exec

import "testing"

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
	got, err := InterpreterCmd("pwsh.exe")
	if err != nil {
		t.Fatal(err)
	}
	want := "powershell -NoProfile -NonInteractive -Command -"
	if got != want {
		t.Errorf("InterpreterCmd(pwsh.exe) = %q, want %q", got, want)
	}
}
