package hosts

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildBlock(t *testing.T) {
	block := buildBlock("dev.local", []string{"b.dev.local", "a.dev.local"})
	expected := "# dev-local:BEGIN\n# Managed by caddy-dev-local — do not edit.\n127.0.0.1    dev.local\n127.0.0.1    a.dev.local\n127.0.0.1    b.dev.local\n# dev-local:END\n"
	if block != expected {
		t.Errorf("buildBlock mismatch\ngot:\n%s\nwant:\n%s", block, expected)
	}
}

func TestBuildBlockEmpty(t *testing.T) {
	block := buildBlock("dev.local", nil)
	expected := "# dev-local:BEGIN\n# Managed by caddy-dev-local — do not edit.\n127.0.0.1    dev.local\n# dev-local:END\n"
	if block != expected {
		t.Errorf("buildBlock empty mismatch\ngot:\n%s\nwant:\n%s", block, expected)
	}
}

func TestBuildBlockTDLDedup(t *testing.T) {
	block := buildBlock("dev.local", []string{"dev.local", "a.dev.local"})
	expected := "# dev-local:BEGIN\n# Managed by caddy-dev-local — do not edit.\n127.0.0.1    dev.local\n127.0.0.1    a.dev.local\n# dev-local:END\n"
	if block != expected {
		t.Errorf("buildBlock dedup mismatch\ngot:\n%s\nwant:\n%s", block, expected)
	}
}

func TestSyncCreatesNewBlock(t *testing.T) {
	path := tempHosts(t, "")
	if err := syncToFile(path, "dev.local", []string{"foo.dev.local"}); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(path)
	if !strings.Contains(string(content), "foo.dev.local") {
		t.Error("expected domain in hosts file")
	}
	if !strings.Contains(string(content), beginMarker) {
		t.Error("expected begin marker")
	}
	if !strings.Contains(string(content), endMarker) {
		t.Error("expected end marker")
	}
}

func TestSyncReplacesExistingBlock(t *testing.T) {
	initial := "10.0.0.1 other.dev.local\n# dev-local:BEGIN\n# Managed by caddy-dev-local — do not edit.\n127.0.0.1    old.dev.local\n# dev-local:END\n"
	path := tempHosts(t, initial)

	if err := syncToFile(path, "dev.local", []string{"new.dev.local"}); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(path)
	if strings.Contains(string(content), "old.dev.local") {
		t.Error("old block domain should have been replaced")
	}
	if !strings.Contains(string(content), "new.dev.local") {
		t.Error("expected new domain")
	}
	if !strings.Contains(string(content), "other.dev.local") {
		t.Error("non-block entries should be preserved")
	}
}

func TestSyncNoOpWhenUnchanged(t *testing.T) {
	initial := "# dev-local:BEGIN\n# Managed by caddy-dev-local — do not edit.\n127.0.0.1    foo.dev.local\n# dev-local:END\n"
	path := tempHosts(t, initial)

	info1, _ := os.Stat(path)
	modTime1 := info1.ModTime()

	if err := syncToFile(path, "dev.local", []string{"foo.dev.local"}); err != nil {
		t.Fatal(err)
	}

	info2, _ := os.Stat(path)
	modTime2 := info2.ModTime()
	if !modTime1.Equal(modTime2) {
		t.Error("file should not have been modified when content unchanged")
	}
}

func TestSyncSortsDomains(t *testing.T) {
	path := tempHosts(t, "")
	if err := syncToFile(path, "dev.local", []string{"z.dev.local", "a.dev.local", "m.dev.local"}); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(path)
	aIdx := bytes.Index(content, []byte("a.dev.local"))
	zIdx := bytes.Index(content, []byte("z.dev.local"))
	if aIdx > zIdx {
		t.Error("domains should be sorted alphabetically")
	}
}

func TestRemoveDeletesBlock(t *testing.T) {
	initial := "10.0.0.1 keep.dev.local\n# dev-local:BEGIN\n# Managed by caddy-dev-local — do not edit.\n127.0.0.1    remove.dev.local\n# dev-local:END\n"
	path := tempHosts(t, initial)

	if err := removeFromFile(path); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(path)
	if strings.Contains(string(content), "remove.dev.local") {
		t.Error("block should have been removed")
	}
	if !strings.Contains(string(content), "keep.dev.local") {
		t.Error("unrelated entries should be preserved")
	}
}

func TestRemoveOnlyOurBlock(t *testing.T) {
	initial := "10.0.0.1 other.dev.local\n# dev-local:BEGIN\n# Managed by caddy-dev-local — do not edit.\n127.0.0.1    managed.dev.local\n# dev-local:END\n10.0.0.1 another.dev.local\n"
	path := tempHosts(t, initial)

	if err := removeFromFile(path); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(path)
	if strings.Contains(string(content), "managed.dev.local") {
		t.Error("our block should be removed")
	}
	if !strings.Contains(string(content), "other.dev.local") {
		t.Error("other entries should remain")
	}
	if !strings.Contains(string(content), "another.dev.local") {
		t.Error("trailing entries should remain")
	}
}

func TestRemoveNoBlock(t *testing.T) {
	initial := "10.0.0.1 something.dev.local\n"
	path := tempHosts(t, initial)

	if err := removeFromFile(path); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(path)
	if string(content) != initial {
		t.Error("file should be unchanged when no block exists")
	}
}

func TestRemoveNoFile(t *testing.T) {
	if err := removeFromFile("/nonexistent/path/hosts"); err != nil {
		t.Fatal(err)
	}
}

func TestSyncAppendsToExistingContent(t *testing.T) {
	initial := "10.0.0.1 existing.dev.local\n"
	path := tempHosts(t, initial)

	if err := syncToFile(path, "dev.local", []string{"new.dev.local"}); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(path)
	if !strings.Contains(string(content), "existing.dev.local") {
		t.Error("existing entries should be preserved")
	}
	if !strings.Contains(string(content), "new.dev.local") {
		t.Error("new domain should be present")
	}
}

func tempHosts(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}
