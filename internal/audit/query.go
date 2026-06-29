package audit

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shayuc137/sshq/internal/credential"
)

type QueryOpts struct {
	Last      int
	Alias     string
	Operation string
	Warn      func(string)
}

func Query(path string, opts QueryOpts) ([]Entry, error) {
	if path == "" {
		defaultPath, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		path = defaultPath
	} else {
		path = credential.ExpandHome(path)
	}

	files, err := auditFiles(path)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0)
	for _, file := range files {
		fileEntries, err := readEntries(file, opts)
		if err != nil {
			return nil, err
		}
		entries = append(entries, fileEntries...)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entryTime(entries[i]).After(entryTime(entries[j]))
	})
	if opts.Last > 0 && len(entries) > opts.Last {
		entries = entries[:opts.Last]
	}
	if entries == nil {
		return []Entry{}, nil
	}
	return entries, nil
}

func auditFiles(path string) ([]string, error) {
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read audit directory: %w", err)
	}

	base := filepath.Base(path)
	ext := filepath.Ext(base)
	prefix := strings.TrimSuffix(base, ext) + "-"
	files := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == base || (strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ext)) {
			files = append(files, filepath.Join(dir, name))
		}
	}
	sort.Strings(files)
	return files, nil
}

func readEntries(path string, opts QueryOpts) ([]Entry, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	out := make([]Entry, 0)
	line := 0
	for scanner.Scan() {
		line++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			if opts.Warn != nil {
				opts.Warn(fmt.Sprintf("warning: skipped corrupt audit line %s:%d: %v", path, line, err))
			}
			continue
		}
		if !matchesQuery(e, opts) {
			continue
		}
		out = append(out, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan audit log: %w", err)
	}
	return out, nil
}

func matchesQuery(e Entry, opts QueryOpts) bool {
	if opts.Operation != "" && e.Operation != opts.Operation {
		return false
	}
	if opts.Alias == "" {
		return true
	}
	if e.Alias == opts.Alias {
		return true
	}
	for _, alias := range e.Aliases {
		if alias == opts.Alias {
			return true
		}
	}
	return false
}

func entryTime(e Entry) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, e.Timestamp); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, e.Timestamp); err == nil {
		return t
	}
	return time.Time{}
}
