package audit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (l *Logger) rotateIfNeededLocked(entrySize int64) error {
	if l.maxSize <= 0 || l.file == nil {
		return nil
	}
	info, err := l.file.Stat()
	if err != nil {
		return fmt.Errorf("stat audit log: %w", err)
	}
	if info.Size() == 0 || info.Size()+entrySize <= l.maxSize {
		return nil
	}
	return l.rotateLocked()
}

func (l *Logger) rotateLocked() error {
	if l.file != nil {
		if err := l.file.Close(); err != nil {
			return fmt.Errorf("close audit log before rotate: %w", err)
		}
		l.file = nil
	}

	rotated, err := nextRotatedPath(l.path, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := os.Rename(l.path, rotated); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("rotate audit log: %w", err)
	}
	if err := syncDir(filepath.Dir(l.path)); err != nil {
		return err
	}

	f, err := openSecureFile(l.path)
	if err != nil {
		return err
	}
	l.file = f
	return nil
}

func nextRotatedPath(path string, now time.Time) (string, error) {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	stamp := now.Format("20060102-150405")
	candidate := filepath.Join(dir, fmt.Sprintf("%s-%s%s", base, stamp, ext))
	if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
		return candidate, nil
	} else if err != nil {
		return "", fmt.Errorf("stat rotated audit log: %w", err)
	}
	for i := 1; ; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%s-%d%s", base, stamp, i, ext))
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", fmt.Errorf("stat rotated audit log: %w", err)
		}
	}
}
