package ui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jrsearles/caddy-dev-local/discovery"
)

type configSourceFunc func(context.Context) (string, error)

func (f configSourceFunc) RunningConfig(ctx context.Context) (string, error) {
	return f(ctx)
}

func TestApplyWritesReadableArtifactsAndStableVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	p := New("dev.local", dir, configSourceFunc(func(context.Context) (string, error) {
		return `{"apps":{"http":{}}}`, nil
	}))
	update := discovery.Update{
		Snapshot: []*discovery.ContainerInfo{
			{ContainerID: "b", ContainerName: "api", IsRunning: true, Ports: []uint16{443, 80}, SelectedPort: 80},
			{ContainerID: "a", ContainerName: "web", IsRunning: true, Ports: []uint16{80}, SelectedPort: 80},
		},
		Status: discovery.Status{LastRefresh: time.Unix(1700000000, 0)},
	}
	if err := p.Apply(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	firstVersion := readVersion(t, dir)

	update.Snapshot[0], update.Snapshot[1] = update.Snapshot[1], update.Snapshot[0]
	if err := p.Apply(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	if secondVersion := readVersion(t, dir); secondVersion != firstVersion {
		t.Fatalf("version changed for reordered state: %q != %q", secondVersion, firstVersion)
	}
	if err := os.Chmod(filepath.Join(dir, "index.css"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(context.Background(), update); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"index.html", "index.css", "version.json"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0644 {
			t.Errorf("%s mode = %o, want 644", name, info.Mode().Perm())
		}
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0755 {
		t.Errorf("output directory mode = %o, want 755", dirInfo.Mode().Perm())
	}
	page, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `"apps":{"http":{}}`) {
		t.Error("running config missing from page")
	}
}

func TestApplyCachesConfigAcrossSourceFailure(t *testing.T) {
	calls := 0
	p := New("dev.local", t.TempDir(), configSourceFunc(func(context.Context) (string, error) {
		calls++
		if calls == 1 {
			return `{"cached":true}`, nil
		}
		return "", errors.New("caddy unavailable")
	}))

	if err := p.Apply(context.Background(), discovery.Update{}); err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(context.Background(), discovery.Update{}); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(p.outputDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `"cached":true`) {
		t.Error("cached running config missing after source failure")
	}
}

func TestVersionChangesWithRenderedState(t *testing.T) {
	p := New("dev.local", t.TempDir(), nil)
	update := discovery.Update{Snapshot: []*discovery.ContainerInfo{{ContainerID: "a", ContainerName: "web", IsRunning: true, Ports: []uint16{80}}}}
	if err := p.Apply(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	before := readVersion(t, p.outputDir)
	update.Status.LastError = "Docker unavailable"
	if err := p.Apply(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	if after := readVersion(t, p.outputDir); after == before {
		t.Fatal("version did not change with rendered discovery state")
	}
}

func TestCleanupRemovesOnlyGeneratedArtifacts(t *testing.T) {
	dir := t.TempDir()
	p := New("dev.local", dir, nil)
	if err := p.Apply(context.Background(), discovery.Update{}); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(unrelated, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "index.css", "version.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s still exists", name)
		}
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated file removed: %v", err)
	}
}

func readVersion(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "version.json"))
	if err != nil {
		t.Fatal(err)
	}
	var version struct {
		Version string `json:"v"`
	}
	if err := json.Unmarshal(data, &version); err != nil {
		t.Fatal(err)
	}
	return version.Version
}
