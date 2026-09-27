package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSaveLoadDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "nested", "config.json")
	if got, err := Load(path); err != nil || got != (Config{}) {
		t.Fatalf("missing config: %#v, %v", got, err)
	}
	want := Config{Endpoint: "https://localhost:8787", APIKey: "test-private-key", CAFile: "/tmp/test.pem"}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got != want {
		t.Fatalf("loaded config mismatch: %v", err)
	}
	if runtime.GOOS != "windows" {
		for name, mode := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700, filepath.Dir(filepath.Dir(path)): 0700} {
			info, err := os.Stat(name)
			if err != nil || info.Mode().Perm() != mode {
				t.Errorf("unexpected permission for %s: %v", name, err)
			}
		}
	}
	want.APIKey = "replacement-private-key"
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err = Load(path)
	if err != nil || got != want {
		t.Fatalf("replacement config mismatch: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatalf("temporary files left after save: %v", err)
	}
	if err := Delete(path); err != nil {
		t.Fatal(err)
	}
	if err := Delete(path); err != nil {
		t.Fatalf("second deletion: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("config was not deleted: %v", err)
	}
}

func TestMalformedConfig(t *testing.T) {
	for _, input := range []string{
		"", "null", "[]", `"secret-canary"`,
		`{"secret-canary":"not-an-allowed-field"}`,
		`{"endpoint":99,"api_key":"secret-canary"}`,
		`{"endpoint":"https://localhost","api_key":"secret-canary"} {}`,
		`{"endpoint":"https://localhost","api_key":"secret-canary"} garbage`,
		`{"api_key":"secret-canary"`,
		strings.Repeat(" ", maxSize+1),
	} {
		t.Run("invalid", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(input), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path)
			if err == nil || got != (Config{}) {
				t.Fatal("malformed config accepted")
			}
			if strings.Contains(err.Error(), "secret-canary") {
				t.Fatal("error exposes configuration content")
			}
		})
	}
}

func TestFailedSavePreservesExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := Config{Endpoint: "https://localhost", APIKey: "original"}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, Config{APIKey: strings.Repeat("x", maxSize)}); err == nil {
		t.Fatal("oversized configuration accepted")
	}
	if got, err := Load(path); err != nil || got != want {
		t.Fatalf("failed save changed existing file: %v", err)
	}
}

func TestSymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	path := filepath.Join(dir, "config.json")
	want := Config{APIKey: "unchanged"}
	if err := Save(target, want); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation needs Windows privilege")
		}
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load followed symlink")
	}
	if err := Save(path, Config{APIKey: "replacement"}); err == nil {
		t.Fatal("Save accepted symlink")
	}
	if err := Delete(path); err == nil {
		t.Fatal("Delete accepted symlink")
	}
	if got, err := Load(target); err != nil || got != want {
		t.Fatalf("symlink target changed: %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, want); err == nil {
		t.Fatal("Save accepted dangling symlink")
	}
}

func TestDirectoryRejected(t *testing.T) {
	path := t.TempDir()
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted a directory")
	}
	if err := Save(path, Config{}); err == nil {
		t.Fatal("Save accepted a directory")
	}
	if err := Delete(path); err == nil {
		t.Fatal("Delete accepted a directory")
	}
}

func TestUnixPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses ACLs")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := Save(path, Config{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatal("Save changed existing parent permissions")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted public credentials")
	}
	if err := Save(path, Config{}); err == nil {
		t.Fatal("Save accepted public existing credentials")
	}
	if err := Delete(path); err == nil {
		t.Fatal("Delete accepted public existing credentials")
	}
}

func TestDefaultPath(t *testing.T) {
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Skip("user config directory unavailable")
	}
	got, err := DefaultPath()
	if err != nil || got != filepath.Join(dir, "vlno", "config.json") {
		t.Fatalf("unexpected default path: %q, %v", got, err)
	}
}
