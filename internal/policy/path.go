package policy

import (
	"fmt"
	"path"
	"strings"
)

func (c *Checker) CheckLocalPath(alias, localPath string) Decision {
	effective, err := c.effectivePolicy(alias)
	if err != nil {
		return deny(alias, KindLocalPath, ReasonConfigError, err.Error(), localPath)
	}
	if !effective.Enabled || len(effective.LocalPathWhitelist) == 0 {
		return allow(alias, KindLocalPath, localPath)
	}

	allowed, pattern, err := localPathAllowed(localPath, effective.LocalPathWhitelist)
	if err != nil {
		return deny(alias, KindLocalPath, ReasonConfigError, err.Error(), localPath)
	}
	if allowed {
		return allow(alias, KindLocalPath, localPath)
	}
	if c.grants != nil && c.grants.MatchLocalPath(alias, localPath) {
		return allow(alias, KindLocalPath, localPath)
	}
	return deny(alias, KindLocalPath, ReasonLocalPathDenied, pattern, localPath)
}

func (c *Checker) CheckRemotePath(alias, remotePath string) Decision {
	effective, err := c.effectivePolicy(alias)
	if err != nil {
		return deny(alias, KindRemotePath, ReasonConfigError, err.Error(), remotePath)
	}
	if !effective.Enabled || len(effective.RemotePathWhitelist) == 0 {
		return allow(alias, KindRemotePath, remotePath)
	}

	allowed, pattern, err := remotePathAllowed(remotePath, effective.RemotePathWhitelist)
	if err != nil {
		return deny(alias, KindRemotePath, ReasonConfigError, err.Error(), remotePath)
	}
	if allowed {
		return allow(alias, KindRemotePath, remotePath)
	}
	if c.grants != nil && c.grants.MatchRemotePath(alias, remotePath) {
		return allow(alias, KindRemotePath, remotePath)
	}
	return deny(alias, KindRemotePath, ReasonRemotePathDenied, pattern, remotePath)
}

func localPathAllowed(localPath string, whitelist []string) (bool, string, error) {
	normalized, err := normalizeLocalPath(localPath)
	if err != nil {
		return false, "", fmt.Errorf("normalize local path %q: %w", localPath, err)
	}
	for _, entry := range whitelist {
		normalizedEntry, err := normalizeLocalPath(entry)
		if err != nil {
			return false, entry, fmt.Errorf("normalize local whitelist %q: %w", entry, err)
		}
		if localPathWithin(normalized, normalizedEntry) {
			return true, entry, nil
		}
	}
	return false, "", nil
}

func remotePathAllowed(remotePath string, whitelist []string) (bool, string, error) {
	normalizedEntries := make([]string, 0, len(whitelist))
	for _, entry := range whitelist {
		normalizedEntry := cleanRemotePath(entry)
		if !path.IsAbs(normalizedEntry) {
			return false, entry, fmt.Errorf("remote whitelist %q must be absolute", entry)
		}
		normalizedEntries = append(normalizedEntries, normalizedEntry)
	}

	normalized := cleanRemotePath(remotePath)
	if !path.IsAbs(normalized) {
		return false, "absolute remote path required", nil
	}
	for i, normalizedEntry := range normalizedEntries {
		if remotePathWithin(normalized, normalizedEntry) {
			return true, whitelist[i], nil
		}
	}
	return false, "", nil
}

func remoteWhitelistValid(whitelist []string) error {
	for _, entry := range whitelist {
		if !path.IsAbs(cleanRemotePath(entry)) {
			return fmt.Errorf("remote whitelist %q must be absolute", entry)
		}
	}
	return nil
}

func cleanRemotePath(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if p == "" {
		return "."
	}
	return path.Clean(p)
}

func remotePathWithin(candidate, prefix string) bool {
	candidate = cleanRemotePath(candidate)
	prefix = cleanRemotePath(prefix)
	if candidate == prefix {
		return true
	}
	if prefix == "/" {
		return strings.HasPrefix(candidate, "/")
	}
	return strings.HasPrefix(candidate, strings.TrimRight(prefix, "/")+"/")
}
