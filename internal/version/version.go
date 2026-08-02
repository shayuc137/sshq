package version

import (
	"fmt"
	"runtime/debug"
	"strings"
	"time"
)

var (
	Version = "0.4.1"
	Commit  = ""
	Date    = ""
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	// go install @vX.Y.Z sets a clean semver tag; local builds produce (devel) or pseudo-versions.
	if v := info.Main.Version; isRelease(v) {
		Version = v
	}
	settings := make(map[string]string)
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}
	if Commit == "" {
		if rev, ok := settings["vcs.revision"]; ok {
			Commit = truncate(rev, 7)
		} else if hash, ts, ok := parsePseudoVersion(info.Main.Version); ok {
			// go-install builds carry no vcs.* settings; a pseudo-version is
			// the only remaining source of commit/date.
			Commit = truncate(hash, 7)
			if Date == "" {
				Date = ts
			}
		}
	}
	if Date == "" {
		if t, ok := settings["vcs.time"]; ok {
			Date = t
		}
	}
	if dirty, ok := settings["vcs.modified"]; ok && dirty == "true" {
		Commit += "-dirty"
	}
}

// String omits commit/date when unavailable (go install from a clean tag has
// no VCS info; the tag in "version" already pins the source) instead of
// printing noise like "unknown".
func String() string {
	var meta []string
	if Commit != "" {
		meta = append(meta, Commit)
	}
	if Date != "" {
		meta = append(meta, Date)
	}
	if len(meta) == 0 {
		return fmt.Sprintf("sshq v%s", Number())
	}
	return fmt.Sprintf("sshq v%s (%s)", Number(), strings.Join(meta, ", "))
}

// Number returns the stable semantic version without build metadata.
func Number() string {
	return normalizedVersion()
}

// Map keeps "version" always present; "commit" and "date" appear only when
// known, so JSON consumers never see placeholder values.
func Map() map[string]string {
	m := map[string]string{"version": Number()}
	if Commit != "" {
		m["commit"] = Commit
	}
	if Date != "" {
		m["date"] = Date
	}
	return m
}

func normalizedVersion() string {
	return strings.TrimPrefix(Version, "v")
}

// parsePseudoVersion extracts the commit hash and UTC timestamp from a Go
// pseudo-version like "v0.2.1-0.20260710023504-abcdef123456".
func parsePseudoVersion(v string) (hash, date string, ok bool) {
	parts := strings.Split(v, "-")
	if len(parts) < 3 {
		return "", "", false
	}
	hash = parts[len(parts)-1]
	if len(hash) != 12 || !isHex(hash) {
		return "", "", false
	}
	ts := parts[len(parts)-2]
	if i := strings.LastIndexByte(ts, '.'); i >= 0 {
		ts = ts[i+1:]
	}
	t, err := time.Parse("20060102150405", ts)
	if err != nil {
		return "", "", false
	}
	return hash, t.UTC().Format(time.RFC3339), true
}

func isHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// isRelease returns true for clean semver tags like "v0.2.0", false for
// "(devel)", pseudo-versions, or empty strings.
func isRelease(v string) bool {
	if v == "" || v == "(devel)" {
		return false
	}
	return !strings.Contains(v, "-0.") && !strings.Contains(v, "+")
}
