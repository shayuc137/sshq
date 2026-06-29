//go:build windows

package policy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func normalizeLocalPath(p string) (string, error) {
	expanded := expandLocalHome(p)
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	resolved, err := evalSymlinksBestEffort(filepath.Clean(abs))
	if err != nil {
		return "", err
	}
	return strings.ToLower(filepath.Clean(resolved)), nil
}

func localPathWithin(candidate, prefix string) bool {
	candidate = strings.ToLower(filepath.Clean(candidate))
	prefix = strings.ToLower(filepath.Clean(prefix))
	if candidate == prefix {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(candidate, strings.TrimRight(prefix, `\/`)+sep)
}

func evalSymlinksBestEffort(p string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	current := p
	var suffix []string
	for {
		suffix = append([]string{filepath.Base(current)}, suffix...)
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(p), nil
		}
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			parts := append([]string{resolved}, suffix...)
			return filepath.Join(parts...), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		current = parent
	}
}

func expandLocalHome(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
