package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
)

const maxBinarySize = 128 << 20

func extractBinary(archivePath, archiveName, goos, dir string) (string, error) {
	binaryName := "sshq"
	if goos == "windows" {
		binaryName = "sshq.exe"
	}
	if len(archiveName) >= 4 && archiveName[len(archiveName)-4:] == ".zip" {
		return extractZip(archivePath, binaryName, dir)
	}
	return extractTarGz(archivePath, binaryName, dir)
}

func extractTarGz(archivePath, binaryName, dir string) (staged string, err error) {
	var extracted string
	defer func() {
		if err != nil && extracted != "" {
			_ = os.Remove(extracted)
		}
	}()
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	found := false
	reader := tar.NewReader(gz)
	for {
		header, nextErr := reader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return "", nextErr
		}
		if header.Name != binaryName {
			continue
		}
		if found {
			return "", fmt.Errorf("archive contains more than one %s", binaryName)
		}
		if header.Typeflag != tar.TypeReg {
			return "", fmt.Errorf("archive entry %s is not a regular file", binaryName)
		}
		staged, err = writeStagedBinary(reader, header.Size, dir)
		if err != nil {
			return "", err
		}
		extracted = staged
		found = true
	}
	if !found {
		return "", fmt.Errorf("archive does not contain %s", binaryName)
	}
	return staged, nil
}

func extractZip(archivePath, binaryName, dir string) (staged string, err error) {
	var extracted string
	defer func() {
		if err != nil && extracted != "" {
			_ = os.Remove(extracted)
		}
	}()
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	found := false
	for _, entry := range reader.File {
		if entry.Name != binaryName {
			continue
		}
		if found {
			return "", fmt.Errorf("archive contains more than one %s", binaryName)
		}
		if !entry.Mode().IsRegular() {
			return "", fmt.Errorf("archive entry %s is not a regular file", binaryName)
		}
		body, err := entry.Open()
		if err != nil {
			return "", err
		}
		staged, err = writeStagedBinary(body, int64(entry.UncompressedSize64), dir)
		body.Close()
		if err != nil {
			return "", err
		}
		extracted = staged
		found = true
	}
	if !found {
		return "", fmt.Errorf("archive does not contain %s", binaryName)
	}
	return staged, nil
}

func writeStagedBinary(reader io.Reader, size int64, dir string) (path string, err error) {
	if size < 0 || size > maxBinarySize {
		return "", fmt.Errorf("binary exceeds the %d-byte limit", maxBinarySize)
	}
	file, err := os.CreateTemp(dir, ".sshq-staged-*")
	if err != nil {
		return "", err
	}
	path = file.Name()
	ok := false
	defer func() {
		if !ok {
			file.Close()
			os.Remove(path)
		}
	}()
	if err := file.Chmod(0755); err != nil {
		return "", err
	}
	written, err := io.Copy(file, io.LimitReader(reader, maxBinarySize+1))
	if err != nil {
		return "", err
	}
	if written > maxBinarySize || (size >= 0 && written != size) {
		return "", fmt.Errorf("binary size mismatch: expected %d, got %d", size, written)
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}
