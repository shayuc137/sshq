package updater

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/mod/semver"
)

const latestReleaseURL = "https://api.github.com/repos/shayuc137/sshq/releases/latest"

type Mode uint8

const (
	ModeCheck Mode = iota
	ModeApply
)

type Result struct {
	CurrentVersion   string `json:"current_version"`
	LatestVersion    string `json:"latest_version"`
	UpdateAvailable  bool   `json:"update_available"`
	AssetName        string `json:"asset,omitempty"`
	ChecksumVerified bool   `json:"checksum_verified"`
	BinaryUpdated    bool   `json:"binary_updated"`
	TargetPath       string `json:"target_path,omitempty"`
}

type Updater struct {
	currentVersion string
	apiURL         string
	httpClient     *http.Client
	goos           string
	goarch         string
	targetPath     string
	cacheDir       string
	allowTestHTTP  bool
}

func New(currentVersion string) *Updater {
	u := &Updater{
		currentVersion: currentVersion,
		apiURL:         latestReleaseURL,
		goos:           runtime.GOOS,
		goarch:         runtime.GOARCH,
	}
	u.httpClient = &http.Client{CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		return u.validateURL(req.URL)
	}}
	return u
}

func (u *Updater) Run(ctx context.Context, mode Mode) (Result, error) {
	result := Result{CurrentVersion: strings.TrimPrefix(u.currentVersion, "v")}
	release, err := u.fetchRelease(ctx)
	if err != nil {
		return result, err
	}

	current, err := validVersion(u.currentVersion, "current")
	if err != nil {
		return result, err
	}
	latest, err := validVersion(release.TagName, "latest")
	if err != nil {
		return result, err
	}
	result.LatestVersion = strings.TrimPrefix(latest, "v")
	result.UpdateAvailable = semver.Compare(current, latest) < 0
	if !result.UpdateAvailable {
		return result, nil
	}

	archive, checksums, err := selectAssets(release, result.LatestVersion, u.goos, u.goarch)
	if err != nil {
		return result, err
	}
	for _, selected := range []asset{archive, checksums} {
		parsed, parseErr := url.Parse(selected.BrowserDownloadURL)
		if parseErr != nil {
			return result, fmt.Errorf("parse release asset URL for %s: %w", selected.Name, parseErr)
		}
		if err := u.validateURL(parsed); err != nil {
			return result, err
		}
	}
	result.AssetName = archive.Name
	if mode == ModeCheck {
		return result, nil
	}

	target, err := u.executablePath()
	if err != nil {
		return result, err
	}
	result.TargetPath = target

	staged, err := u.stageBinary(ctx, archive, checksums, result.LatestVersion)
	if err != nil {
		return result, err
	}
	result.ChecksumVerified = true

	if err := replaceBinary(staged, target); err != nil {
		var permissionErr *PermissionError
		if !errors.As(err, &permissionErr) {
			_ = os.Remove(staged)
		}
		return result, err
	}
	_ = os.Remove(staged)
	_ = os.Remove(filepath.Dir(staged))
	result.BinaryUpdated = true
	return result, nil
}

func validVersion(value, label string) (string, error) {
	v := strings.TrimSpace(value)
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return "", fmt.Errorf("%s version %q is not valid semantic versioning", label, value)
	}
	return v, nil
}

func (u *Updater) executablePath() (string, error) {
	path := u.targetPath
	if path == "" {
		var err error
		path, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve current executable: %w", err)
		}
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlinks: %w", err)
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	return abs, nil
}
