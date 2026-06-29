//go:build windows

package audit

import "io/fs"

func setFilePermission(string, fs.FileMode) error { return nil }

func setDirPermission(string, fs.FileMode) error { return nil }

func syncDir(string) error { return nil }
