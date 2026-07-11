package remote

import "testing"

func TestSuspectStaleProfile(t *testing.T) {
	tests := []struct {
		name    string
		profile *Profile
		stdout  string
		stderr  string
		want    bool
	}{
		{name: "powershell missing on cmd", profile: &Profile{Shell: PowerShell}, stderr: "'PowerShell' IS NOT RECOGNIZED as a command", want: true},
		{name: "powershell missing on posix", profile: &Profile{Shell: PowerShell}, stderr: "sh: PowerShell: NOT FOUND", want: true},
		{name: "powershell ordinary failure", profile: &Profile{Shell: PowerShell}, stderr: "Get-Item: path not found"},
		{name: "cmd receives clixml stdout", profile: &Profile{Shell: Cmd}, stdout: "#< CLIXML\r\n<Objs />", want: true},
		{name: "cmd receives clixml stderr", profile: &Profile{Shell: Cmd}, stderr: "#< clixml\n<Objs />", want: true},
		{name: "cmd clixml is not prefix", profile: &Profile{Shell: Cmd}, stdout: "error: #< CLIXML"},
		{name: "bash receives cmd diagnostic", profile: &Profile{Shell: Bash}, stderr: "FOO IS NOT RECOGNIZED AS AN INTERNAL OR EXTERNAL COMMAND", want: true},
		{name: "ash receives cmd diagnostic", profile: &Profile{Shell: Ash}, stderr: "foo is not recognized as an internal or external command", want: true},
		{name: "zsh ordinary command failure", profile: &Profile{Shell: Zsh}, stderr: "zsh: command not found: foo"},
		{name: "sh ordinary exit", profile: &Profile{Shell: Sh}, stderr: "permission denied"},
		{name: "nil profile", stderr: "'powershell' is not recognized", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SuspectStaleProfile(tt.profile, tt.stdout, tt.stderr); got != tt.want {
				t.Fatalf("SuspectStaleProfile() = %v, want %v", got, tt.want)
			}
		})
	}
}
