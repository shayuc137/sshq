package version

import "testing"

func TestStringAddsSingleVPrefix(t *testing.T) {
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	t.Cleanup(func() {
		Version, Commit, Date = oldVersion, oldCommit, oldDate
	})

	Version = "v0.1.0"
	Commit = "abc1234"
	Date = "2026-06-28"

	if got := String(); got != "sshq v0.1.0 (abc1234, 2026-06-28)" {
		t.Fatalf("String = %q", got)
	}
	if got := Map()["version"]; got != "0.1.0" {
		t.Fatalf("Map version = %q, want 0.1.0", got)
	}
}
