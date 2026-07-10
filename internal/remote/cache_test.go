package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCacheLoadCorruptFileWarnsAndRebuilds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	if err := os.WriteFile(path, []byte("{bad json"), 0644); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	cache, err := NewCache(time.Hour,
		WithCachePath(path),
		WithCacheInfo(func(msg string) { warnings = append(warnings, msg) }),
	)
	if err != nil {
		t.Fatalf("NewCache returned error: %v", err)
	}
	if len(cache.data) != 0 {
		t.Fatalf("cache data len = %d, want 0", len(cache.data))
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "corrupt") {
		t.Fatalf("warnings = %v, want corrupt warning", warnings)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "{}" {
		t.Fatalf("rebuilt cache = %q, want {}", string(data))
	}
}

func TestCacheLoadsLegacyProfileWithoutExecutablePaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	data := `{"host.example:22":{"os":"windows","shell":"powershell","temp_dir":"C:\\Temp","detected_at":4102444800}}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	cache, err := NewCache(24*time.Hour, WithCachePath(path))
	if err != nil {
		t.Fatal(err)
	}
	profile, ok := cache.Get("host.example", "22")
	if !ok || profile.TempDir != `C:\Temp` {
		t.Fatalf("legacy profile = %+v, ok=%v", profile, ok)
	}
	if profile.PowerShellPath != "" || profile.PwshPath != "" {
		t.Fatalf("legacy executable paths should be empty: %+v", profile)
	}
}

func TestCachePathEnvOverridesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env-profiles.json")
	t.Setenv(CachePathEnv, path)

	cache, err := NewCache(time.Hour)
	if err != nil {
		t.Fatalf("NewCache returned error: %v", err)
	}
	if cache.path != path {
		t.Fatalf("cache path = %q, want %q", cache.path, path)
	}
}

func TestCacheSaveWarnsOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.json")
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	cache, err := NewCache(time.Hour,
		WithCachePath(path),
		WithCacheInfo(func(msg string) { warnings = append(warnings, msg) }),
	)
	if err != nil {
		t.Fatalf("NewCache returned error: %v", err)
	}

	cache.Save()
	found := false
	for _, warning := range warnings {
		if strings.Contains(warning, "save failed") || strings.Contains(warning, "load failed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want load/save warning", warnings)
	}
}
