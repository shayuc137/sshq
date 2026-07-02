package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

var (
	Version = "0.2.0"
	Commit  = ""
	Date    = ""
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	// go install @v0.2.0 sets a clean semver tag; local builds produce (devel) or pseudo-versions.
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

func String() string {
	return fmt.Sprintf("sshq v%s (%s, %s)", normalizedVersion(), fallback(Commit), fallback(Date))
}

func Map() map[string]string {
	return map[string]string{
		"version": normalizedVersion(),
		"commit":  fallback(Commit),
		"date":    fallback(Date),
	}
}

func normalizedVersion() string {
	return strings.TrimPrefix(Version, "v")
}

func fallback(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
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
