package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"
)

const maxMetadataSize = 4 << 20

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func (u *Updater) fetchRelease(ctx context.Context) (release, error) {
	data, err := u.downloadBytes(ctx, u.apiURL, maxMetadataSize)
	if err != nil {
		return release{}, fmt.Errorf("query latest sshq release: %w", err)
	}
	var value release
	if err := json.Unmarshal(data, &value); err != nil {
		return release{}, fmt.Errorf("decode latest sshq release: %w", err)
	}
	if value.TagName == "" {
		return release{}, fmt.Errorf("latest sshq release has no tag_name")
	}
	return value, nil
}

func selectAssets(value release, version, goos, goarch string) (asset, asset, error) {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	if (goos != "linux" && goos != "darwin" && goos != "windows") || (goarch != "amd64" && goarch != "arm64") {
		return asset{}, asset{}, fmt.Errorf("unsupported update platform %s/%s", goos, goarch)
	}

	stableName := fmt.Sprintf("sshq_%s_%s%s", goos, goarch, ext)
	versionedName := fmt.Sprintf("sshq_%s_%s_%s%s", version, goos, goarch, ext)
	var stable, versioned, checksums asset
	for _, candidate := range value.Assets {
		switch candidate.Name {
		case stableName:
			stable = candidate
		case versionedName:
			versioned = candidate
		case "checksums.txt":
			checksums = candidate
		}
	}
	selected := stable
	if selected.Name == "" {
		selected = versioned
	}
	if selected.Name == "" {
		return asset{}, asset{}, fmt.Errorf("release %s has no asset for %s/%s", value.TagName, goos, goarch)
	}
	if checksums.Name == "" {
		return asset{}, asset{}, fmt.Errorf("release %s has no checksums.txt asset", value.TagName)
	}
	if err := validateAssetURL(selected); err != nil {
		return asset{}, asset{}, err
	}
	if err := validateAssetURL(checksums); err != nil {
		return asset{}, asset{}, err
	}
	return selected, checksums, nil
}

func validateAssetURL(value asset) error {
	parsed, err := url.Parse(value.BrowserDownloadURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("release asset %q has an invalid download URL", value.Name)
	}
	if path.Base(parsed.Path) != value.Name {
		return fmt.Errorf("release asset %q URL does not end with its filename", value.Name)
	}
	return nil
}

func (u *Updater) validateURL(value *url.URL) error {
	if u.allowTestHTTP && (value.Scheme == "http" || value.Scheme == "https") {
		return nil
	}
	if value.Scheme != "https" {
		return fmt.Errorf("refusing non-HTTPS update URL %q", value.String())
	}
	host := strings.ToLower(value.Hostname())
	if host == "api.github.com" || host == "github.com" || host == "release-assets.githubusercontent.com" || strings.HasSuffix(host, ".githubusercontent.com") {
		return nil
	}
	return fmt.Errorf("refusing update redirect to untrusted host %q", host)
}
