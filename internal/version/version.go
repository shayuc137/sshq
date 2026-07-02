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
