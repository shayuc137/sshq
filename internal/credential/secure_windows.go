//go:build windows

package credential

import "io/fs"

func setFilePermission(string, fs.FileMode) error { return nil }

func setDirPermission(string, fs.FileMode) error { return nil }

func checkFilePermission(string) error { return nil }

func syncDir(string) error { return nil }
