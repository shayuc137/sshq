package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidVersion(t *testing.T) {
	tests := []struct {
		value string
		want  string
		ok    bool
	}{
		{value: "0.3.0", want: "v0.3.0", ok: true},
		{value: "v1.2.3-rc.1", want: "v1.2.3-rc.1", ok: true},
		{value: "devel", ok: false},
	}
	for _, tt := range tests {
		got, err := validVersion(tt.value, "test")
		if (err == nil) != tt.ok || got != tt.want {
			t.Fatalf("validVersion(%q) = %q, %v", tt.value, got, err)
		}
	}
}

func TestSelectAssetsPrefersStableAndFallsBack(t *testing.T) {
	value := release{TagName: "v0.4.0", Assets: []asset{
		{Name: "sshq_0.4.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://github.com/o/r/releases/download/v0.4.0/sshq_0.4.0_linux_amd64.tar.gz"},
		{Name: "sshq_linux_amd64.tar.gz", BrowserDownloadURL: "https://github.com/o/r/releases/download/v0.4.0/sshq_linux_amd64.tar.gz"},
		{Name: "checksums.txt", BrowserDownloadURL: "https://github.com/o/r/releases/download/v0.4.0/checksums.txt"},
	}}
	got, _, err := selectAssets(value, "0.4.0", "linux", "amd64")
	if err != nil || got.Name != "sshq_linux_amd64.tar.gz" {
		t.Fatalf("stable selection = %+v, %v", got, err)
	}
	value.Assets = append(value.Assets[:1], value.Assets[2:]...)
	got, _, err = selectAssets(value, "0.4.0", "linux", "amd64")
	if err != nil || got.Name != "sshq_0.4.0_linux_amd64.tar.gz" {
		t.Fatalf("versioned fallback = %+v, %v", got, err)
	}
}

func TestSelectAssetsSupportsReleaseMatrix(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			ext := ".tar.gz"
			if goos == "windows" {
				ext = ".zip"
			}
			name := fmt.Sprintf("sshq_%s_%s%s", goos, goarch, ext)
			value := release{TagName: "v0.4.0", Assets: []asset{
				{Name: name, BrowserDownloadURL: "https://github.com/o/r/releases/download/v0.4.0/" + name},
				{Name: "checksums.txt", BrowserDownloadURL: "https://github.com/o/r/releases/download/v0.4.0/checksums.txt"},
			}}
			got, _, err := selectAssets(value, "0.4.0", goos, goarch)
			if err != nil || got.Name != name {
				t.Fatalf("%s/%s selection = %+v, %v", goos, goarch, got, err)
			}
		}
	}
}

func TestRunCheckStates(t *testing.T) {
	for _, tt := range []struct {
		name      string
		current   string
		available bool
	}{
		{name: "current", current: "0.4.0"},
		{name: "local newer", current: "0.5.0"},
		{name: "available", current: "0.3.0", available: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newReleaseFixture(t, "linux", []byte("new binary"))
			defer fixture.Close()
			u := fixture.Updater(tt.current, t.TempDir(), filepath.Join(t.TempDir(), "sshq"))
			result, err := u.Run(context.Background(), ModeCheck)
			if err != nil {
				t.Fatal(err)
			}
			if result.UpdateAvailable != tt.available || result.LatestVersion != "0.4.0" {
				t.Fatalf("result = %+v", result)
			}
			if fixture.AssetRequests() != 0 {
				t.Fatalf("check mode downloaded %d assets", fixture.AssetRequests())
			}
		})
	}
}

func TestRunApplyReplacesBinaryAfterChecksum(t *testing.T) {
	goos := "linux"
	if runtime.GOOS == "windows" {
		goos = "windows"
	}
	newBinary := []byte("new verified binary")
	fixture := newReleaseFixture(t, goos, newBinary)
	defer fixture.Close()
	target := filepath.Join(t.TempDir(), "sshq")
	if goos == "windows" {
		target += ".exe"
	}
	if err := os.WriteFile(target, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	u := fixture.Updater("0.3.0", t.TempDir(), target)
	result, err := u.Run(context.Background(), ModeApply)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, newBinary) {
		t.Fatalf("target = %q, %v", got, err)
	}
	if !result.BinaryUpdated || !result.ChecksumVerified {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunApplyChecksumMismatchPreservesTarget(t *testing.T) {
	fixture := newReleaseFixture(t, "linux", []byte("new binary"))
	defer fixture.Close()
	fixture.badChecksum = true
	target := filepath.Join(t.TempDir(), "sshq")
	if err := os.WriteFile(target, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	u := fixture.Updater("0.3.0", t.TempDir(), target)
	_, err := u.Run(context.Background(), ModeApply)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error = %v", err)
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil || string(got) != "old binary" {
		t.Fatalf("target changed to %q, %v", got, readErr)
	}
}

func TestRunApplyMalformedArchivePreservesTarget(t *testing.T) {
	fixture := newReleaseFixture(t, "linux", []byte("new binary"))
	defer fixture.Close()
	fixture.archive = []byte("not a tar.gz archive")
	target := filepath.Join(t.TempDir(), "sshq")
	if err := os.WriteFile(target, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	u := fixture.Updater("0.3.0", t.TempDir(), target)
	_, err := u.Run(context.Background(), ModeApply)
	if err == nil || !strings.Contains(err.Error(), "extract") {
		t.Fatalf("error = %v", err)
	}
	assertFileContent(t, target, "old binary")
}

func TestRunApplyCanceledDownloadPreservesTarget(t *testing.T) {
	fixture := newReleaseFixture(t, "linux", []byte("new binary"))
	defer fixture.Close()
	fixture.blockArchive = true
	target := filepath.Join(t.TempDir(), "sshq")
	if err := os.WriteFile(target, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	u := fixture.Updater("0.3.0", t.TempDir(), target)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := u.Run(ctx, ModeApply)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline", err)
	}
	assertFileContent(t, target, "old binary")
}

func TestValidateURLRejectsUntrustedHost(t *testing.T) {
	u := New("0.3.0")
	parsed, err := url.Parse("https://example.com/sshq_linux_amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if err := u.validateURL(parsed); err == nil || !strings.Contains(err.Error(), "untrusted host") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseChecksumRejectsDuplicate(t *testing.T) {
	hash := strings.Repeat("a", 64)
	_, err := parseChecksum([]byte(hash+"  asset\n"+hash+"  asset\n"), "asset")
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("error = %v", err)
	}
}

func TestReplaceBinaryRollsBackFailedSwap(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sshq")
	staged := filepath.Join(dir, "staged")
	if err := os.WriteFile(target, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	calls := 0
	ops := replaceOps{
		remove: os.Remove,
		rename: func(oldPath, newPath string) error {
			calls++
			if calls == 2 {
				return errors.New("injected swap failure")
			}
			return os.Rename(oldPath, newPath)
		},
	}
	if err := replaceBinaryWithOps(staged, target, ops); err == nil || !strings.Contains(err.Error(), "install replacement") {
		t.Fatalf("error = %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "old" {
		t.Fatalf("target = %q, %v", got, err)
	}
}

func TestReplaceBinaryPreservesRecoveryFilesWhenRollbackFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sshq")
	staged := filepath.Join(dir, "staged")
	if err := os.WriteFile(target, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	calls := 0
	ops := replaceOps{
		remove: os.Remove,
		rename: func(oldPath, newPath string) error {
			calls++
			if calls >= 2 {
				return fmt.Errorf("injected rename failure %d", calls)
			}
			return os.Rename(oldPath, newPath)
		},
	}
	err := replaceBinaryWithOps(staged, target, ops)
	var rollbackErr *RollbackError
	if !errors.As(err, &rollbackErr) {
		t.Fatalf("error = %v, want RollbackError", err)
	}
	for _, path := range []string{rollbackErr.OldPath, rollbackErr.NewPath} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("recovery path %s missing: %v", path, statErr)
		}
	}
}

type releaseFixture struct {
	*httptest.Server
	t            *testing.T
	goos         string
	archiveName  string
	archive      []byte
	badChecksum  bool
	blockArchive bool
	requests     atomic.Int64
}

func newReleaseFixture(t *testing.T, goos string, binary []byte) *releaseFixture {
	t.Helper()
	ext := ".tar.gz"
	archive := tarGzArchive(t, map[string][]byte{"sshq": binary})
	if goos == "windows" {
		ext = ".zip"
		archive = zipArchive(t, map[string][]byte{"sshq.exe": binary})
	}
	f := &releaseFixture{t: t, goos: goos, archiveName: fmt.Sprintf("sshq_%s_amd64%s", goos, ext), archive: archive}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serveHTTP))
	return f
}

func (f *releaseFixture) Updater(current, cacheDir, target string) *Updater {
	u := New(current)
	u.apiURL = f.URL + "/latest"
	u.httpClient = f.Client()
	u.goos = f.goos
	u.goarch = "amd64"
	u.cacheDir = cacheDir
	u.targetPath = target
	u.allowTestHTTP = true
	return u
}

func (f *releaseFixture) AssetRequests() int { return int(f.requests.Load()) }

func (f *releaseFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/latest":
		assets := []asset{
			{Name: f.archiveName, BrowserDownloadURL: f.URL + "/" + f.archiveName, Size: int64(len(f.archive))},
			{Name: "checksums.txt", BrowserDownloadURL: f.URL + "/checksums.txt"},
		}
		_ = json.NewEncoder(w).Encode(release{TagName: "v0.4.0", Assets: assets})
	case "/" + f.archiveName:
		f.requests.Add(1)
		if f.blockArchive {
			<-r.Context().Done()
			return
		}
		_, _ = w.Write(f.archive)
	case "/checksums.txt":
		f.requests.Add(1)
		sum := sha256.Sum256(f.archive)
		encoded := hex.EncodeToString(sum[:])
		if f.badChecksum {
			encoded = strings.Repeat("0", 64)
		}
		fmt.Fprintf(w, "%s  %s\n", encoded, f.archiveName)
	default:
		http.NotFound(w, r)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s = %q, %v; want %q", path, got, err, want)
	}
}

func tarGzArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0755)
		entry, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
