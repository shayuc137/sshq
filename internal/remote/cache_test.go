package remote

import (
	"encoding/json"
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

func TestCacheGetReloadsAfterAnotherInstancePut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	a := newTestCache(t, path)
	b := newTestCache(t, path)

	a.Put("a.example", "22", testProfile())
	if _, ok := b.Get("a.example", "22"); !ok {
		t.Fatal("second cache did not observe first cache put")
	}
}

func TestCacheInterleavedPutsPreserveBothEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	a := newTestCache(t, path)
	b := newTestCache(t, path)

	a.Put("a.example", "22", testProfile())
	b.Put("b.example", "22", testProfile())

	profiles := readCacheFile(t, path)
	if len(profiles) != 2 || profiles["a.example:22"] == nil || profiles["b.example:22"] == nil {
		t.Fatalf("profiles = %#v, want both entries", profiles)
	}
}

func TestCachePutDoesNotReviveInvalidatedEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	a := newTestCache(t, path)
	a.Put("x.example", "22", testProfile())
	b := newTestCache(t, path)

	a.Invalidate("x.example", "22")
	b.Put("y.example", "22", testProfile())

	profiles := readCacheFile(t, path)
	if profiles["x.example:22"] != nil || profiles["y.example:22"] == nil {
		t.Fatalf("profiles = %#v, want only y entry", profiles)
	}
}

func TestCacheStatFailureFallsBackToMemory(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parentFile, "profiles.json")
	var warnings []string
	cache, err := NewCache(time.Hour,
		WithCachePath(path),
		WithCacheInfo(func(msg string) { warnings = append(warnings, msg) }),
	)
	if err != nil {
		t.Fatal(err)
	}

	cache.Put("memory.example", "22", testProfile())
	if _, ok := cache.Get("memory.example", "22"); !ok {
		t.Fatal("memory entry missing after stat/save failure")
	}
	statWarnings := 0
	for _, warning := range warnings {
		if strings.Contains(warning, "stat failed") {
			statWarnings++
		}
	}
	if statWarnings != 1 {
		t.Fatalf("warnings = %v, want exactly one stat warning", warnings)
	}
}

func TestCacheClearPersistsEmptyMapAndReturnsCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	cache := newTestCache(t, path)
	cache.Put("a.example", "22", testProfile())
	cache.Put("b.example", "22", testProfile())

	if count := cache.Clear(); count != 2 {
		t.Fatalf("Clear() = %d, want 2", count)
	}
	profiles := readCacheFile(t, path)
	if len(profiles) != 0 {
		t.Fatalf("profiles = %#v, want empty map", profiles)
	}
}

func TestCacheReloadsWhenSizeChangesWithSameMtime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	cache := newTestCache(t, path)
	cache.Put("a.example", "22", testProfile())
	originalMtime := cache.fileMtime

	profiles := readCacheFile(t, path)
	profiles["longer-hostname.example:22"] = testProfile()
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, originalMtime, originalMtime); err != nil {
		t.Fatal(err)
	}

	if _, ok := cache.Get("longer-hostname.example", "22"); !ok {
		t.Fatal("cache did not reload after size changed with unchanged mtime")
	}
}

func newTestCache(t *testing.T, path string) *Cache {
	t.Helper()
	cache, err := NewCache(time.Hour, WithCachePath(path))
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

func testProfile() *Profile {
	return &Profile{OS: Linux, Shell: Bash, DetectedAt: time.Now().Unix()}
}

func readCacheFile(t *testing.T, path string) map[string]*Profile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	profiles := make(map[string]*Profile)
	if err := json.Unmarshal(data, &profiles); err != nil {
		t.Fatal(err)
	}
	return profiles
}
