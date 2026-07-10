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

func TestStringOmitsUnknownFields(t *testing.T) {
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	t.Cleanup(func() {
		Version, Commit, Date = oldVersion, oldCommit, oldDate
	})

	Version = "0.2.0"
	Commit = ""
	Date = ""

	if got := String(); got != "sshq v0.2.0" {
		t.Fatalf("String = %q, want sshq v0.2.0", got)
	}
	m := Map()
	if _, ok := m["commit"]; ok {
		t.Fatalf("Map should omit empty commit, got %q", m["commit"])
	}
	if _, ok := m["date"]; ok {
		t.Fatalf("Map should omit empty date, got %q", m["date"])
	}
	if m["version"] != "0.2.0" {
		t.Fatalf("Map version = %q, want 0.2.0", m["version"])
	}
}

func TestParsePseudoVersion(t *testing.T) {
	tests := []struct {
		in       string
		hash     string
		date     string
		ok       bool
	}{
		{"v0.2.1-0.20260710023504-abcdef123456", "abcdef123456", "2026-07-10T02:35:04Z", true},
		{"v0.3.0-beta.1.0.20260701120000-1234567890ab", "1234567890ab", "2026-07-01T12:00:00Z", true},
		{"v0.2.0", "", "", false},
		{"(devel)", "", "", false},
		{"v0.3.0-beta.1", "", "", false},                       // prerelease tag, last part not a hash
		{"v0.2.1-0.20260710023504-ABCDEF123456", "", "", false}, // uppercase never appears in pseudo-version hashes
		{"", "", "", false},
	}
	for _, tt := range tests {
		hash, date, ok := parsePseudoVersion(tt.in)
		if ok != tt.ok || hash != tt.hash || date != tt.date {
			t.Errorf("parsePseudoVersion(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.in, hash, date, ok, tt.hash, tt.date, tt.ok)
		}
	}
}
