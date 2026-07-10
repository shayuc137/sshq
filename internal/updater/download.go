package updater

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxChecksumsSize = 1 << 20
	maxArchiveSize   = 256 << 20
)

func (u *Updater) downloadBytes(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	resp, err := u.request(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("response is larger than %d bytes", limit)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response is larger than %d bytes", limit)
	}
	return data, nil
}

func (u *Updater) request(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if err := u.validateURL(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "sshq/"+strings.TrimPrefix(u.currentVersion, "v"))
	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s returned %s", req.URL.Host, resp.Status)
	}
	return resp, nil
}

func (u *Updater) stageBinary(ctx context.Context, archive, checksums asset, version string) (string, error) {
	checksumData, err := u.downloadBytes(ctx, checksums.BrowserDownloadURL, maxChecksumsSize)
	if err != nil {
		return "", fmt.Errorf("download checksums.txt: %w", err)
	}
	want, err := parseChecksum(checksumData, archive.Name)
	if err != nil {
		return "", err
	}

	dir := u.cacheDir
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve user cache directory: %w", err)
		}
		dir = filepath.Join(base, "sshq", "update")
	}
	dir = filepath.Join(dir, version)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create update cache: %w", err)
	}

	archivePath, got, err := u.downloadArchive(ctx, archive, dir)
	if err != nil {
		return "", err
	}
	defer os.Remove(archivePath)
	if got != want {
		return "", fmt.Errorf("checksum mismatch for %s: expected %s, got %s", archive.Name, want, got)
	}

	staged, err := extractBinary(archivePath, archive.Name, u.goos, dir)
	if err != nil {
		return "", fmt.Errorf("extract %s: %w", archive.Name, err)
	}
	return staged, nil
}

func (u *Updater) downloadArchive(ctx context.Context, value asset, dir string) (path string, digest string, err error) {
	if value.Size < 0 || value.Size > maxArchiveSize {
		return "", "", fmt.Errorf("release asset %s exceeds the %d-byte limit", value.Name, maxArchiveSize)
	}
	resp, err := u.request(ctx, value.BrowserDownloadURL)
	if err != nil {
		return "", "", fmt.Errorf("download %s: %w", value.Name, err)
	}
	defer resp.Body.Close()
	if resp.ContentLength > maxArchiveSize {
		return "", "", fmt.Errorf("release asset %s exceeds the %d-byte limit", value.Name, maxArchiveSize)
	}

	file, err := os.CreateTemp(dir, ".sshq-archive-*")
	if err != nil {
		return "", "", fmt.Errorf("create archive staging file: %w", err)
	}
	path = file.Name()
	ok := false
	defer func() {
		if !ok {
			file.Close()
			os.Remove(path)
		}
	}()

	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, maxArchiveSize+1))
	if err != nil {
		return "", "", fmt.Errorf("write archive staging file: %w", err)
	}
	if written > maxArchiveSize {
		return "", "", fmt.Errorf("release asset %s exceeds the %d-byte limit", value.Name, maxArchiveSize)
	}
	if value.Size > 0 && written != value.Size {
		return "", "", fmt.Errorf("release asset %s size mismatch: expected %d, got %d", value.Name, value.Size, written)
	}
	if err := file.Sync(); err != nil {
		return "", "", fmt.Errorf("sync archive staging file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", "", fmt.Errorf("close archive staging file: %w", err)
	}
	ok = true
	return path, hex.EncodeToString(hash.Sum(nil)), nil
}

func parseChecksum(data []byte, filename string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	var match string
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != filename {
			continue
		}
		if match != "" {
			return "", fmt.Errorf("checksums.txt contains duplicate entries for %s", filename)
		}
		if len(fields[0]) != sha256.Size*2 {
			return "", fmt.Errorf("checksums.txt has an invalid SHA-256 for %s", filename)
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return "", fmt.Errorf("checksums.txt has an invalid SHA-256 for %s", filename)
		}
		match = strings.ToLower(fields[0])
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read checksums.txt: %w", err)
	}
	if match == "" {
		return "", fmt.Errorf("checksums.txt has no entry for %s", filename)
	}
	return match, nil
}
