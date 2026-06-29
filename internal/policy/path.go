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
		if !isRemoteAbs(normalizedEntry) {
			return false, entry, fmt.Errorf("remote whitelist %q must be absolute", entry)
		}
		normalizedEntries = append(normalizedEntries, normalizedEntry)
	}

	normalized := cleanRemotePath(remotePath)
	if !isRemoteAbs(normalized) {
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
		if !isRemoteAbs(cleanRemotePath(entry)) {
			return fmt.Errorf("remote whitelist %q must be absolute", entry)
		}
	}
	return nil
}

// cleanRemotePath normalizes a remote path to a forward-slash canonical form
// while preserving the distinctions POSIX path.Clean would erase for Windows
// targets:
//
//   - Windows drive paths ("C:\Temp", "c:/temp") canonicalize to an upper-case
//     drive letter ("C:/Temp") so prefix matching is drive-case-insensitive.
//   - UNC paths ("\\server\share", "//server/share") keep their leading "//" and
//     server/share boundary, which path.Clean collapses to a single slash.
//   - POSIX paths keep their original behavior.
//
// symlink/junction targets that escape the whitelist remain a known limitation,
// identical to POSIX symlinks under the existing prefix policy.
func cleanRemotePath(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if p == "" {
		return "."
	}
	if drive, rest, ok := splitWinDrive(p); ok {
		// Clean the path component under a synthetic root, then re-attach the
		// drive. path.Clean("/"+rest) collapses "." / ".." / duplicate slashes.
		cleaned := path.Clean("/" + rest)
		if cleaned == "/" {
			return drive + "/"
		}
		return drive + cleaned
	}
	if strings.HasPrefix(p, "//") {
		// UNC: preserve the leading "//", clean the remainder so the
		// server/share boundary survives.
		cleaned := path.Clean("/" + strings.TrimLeft(p, "/"))
		return "/" + cleaned
	}
	return path.Clean(p)
}

// isRemoteAbs reports whether a normalized (forward-slash) remote path is
// absolute for any supported remote OS: POSIX ("/..."), Windows drive
// ("C:/..."), or UNC ("//server/share").
func isRemoteAbs(normalized string) bool {
	if strings.HasPrefix(normalized, "//") {
		return true
	}
	if _, _, ok := splitWinDrive(normalized); ok {
		return true
	}
	return path.IsAbs(normalized)
}

// splitWinDrive recognizes a Windows drive prefix on a forward-slash path and
// returns the canonical upper-case drive ("C:") plus the remainder after the
// drive (no leading slash). ok is false for non-drive paths.
func splitWinDrive(p string) (drive, rest string, ok bool) {
	if len(p) >= 2 && p[1] == ':' && isDriveLetter(p[0]) {
		drive = strings.ToUpper(p[:2])
		rest = strings.TrimPrefix(p[2:], "/")
		return drive, rest, true
	}
	return "", "", false
}

func isDriveLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func remotePathWithin(candidate, prefix string) bool {
	candidate = cleanRemotePath(candidate)
	prefix = cleanRemotePath(prefix)
	if candidate == prefix {
		return true
	}
	if prefix == "/" {
		// POSIX root: every absolute POSIX path is within it, but a Windows
		// drive or UNC path is a different namespace and must not match.
		return strings.HasPrefix(candidate, "/") && !strings.HasPrefix(candidate, "//") &&
			!driveRooted(candidate)
	}
	return strings.HasPrefix(candidate, strings.TrimRight(prefix, "/")+"/")
}

func driveRooted(p string) bool {
	_, _, ok := splitWinDrive(p)
	return ok
}
