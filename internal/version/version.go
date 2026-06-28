package version

import (
	"fmt"
	"strings"
)

var (
	Version = "0.1.0"
	Commit  = "unknown"
	Date    = "unknown"
)

func String() string {
	return fmt.Sprintf("sshq v%s (%s, %s)", normalizedVersion(), Commit, Date)
}

func Map() map[string]string {
	return map[string]string{
		"version": normalizedVersion(),
		"commit":  Commit,
		"date":    Date,
	}
}

func normalizedVersion() string {
	return strings.TrimPrefix(Version, "v")
}
