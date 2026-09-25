package ui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
)

type fakeCaddy struct {
	config    string
	configErr error
	calls     int
	routes    map[string]json.RawMessage
	policies  map[string]json.RawMessage
	cleaned   bool
}

func (f *fakeCaddy) RunningConfig(context.Context) (string, error) {
	f.calls++
	if f.configErr != nil {
		return "", f.configErr
	}
	return f.config, nil
}

func (f *fakeCaddy) Reconcile(_ context.Context, routes, policies map[string]json.RawMessage, _ func(string) bool) error {
	f.routes = routes
	f.policies = policies
	return nil
}

func (f *fakeCaddy) Cleanup(context.Context) error {
	f.cleaned = true
	return nil
}

func TestApplyWritesReadableArtifactsAndStableVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	p := New(&config.Config{TLD: "dev.local"}, dir, &fakeCaddy{config: `{"apps":{"http":{}}}`})
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
	caddy := &fakeCaddy{config: `{"cached":true}`}
	p := New(&config.Config{TLD: "dev.local"}, t.TempDir(), caddy)

	if err := p.Apply(context.Background(), discovery.Update{}); err != nil {
		t.Fatal(err)
	}
	caddy.configErr = errors.New("caddy unavailable")
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
	p := New(&config.Config{TLD: "dev.local"}, t.TempDir(), nil)
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
	caddy := &fakeCaddy{}
	p := New(&config.Config{TLD: "dev.local"}, dir, caddy)
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
	if !caddy.cleaned {
		t.Error("Caddy resources were not cleaned up")
	}
}

func TestApplyRegistersIndexRouteAndTLS(t *testing.T) {
	caddy := &fakeCaddy{}
	dir := t.TempDir()
	p := New(&config.Config{TLD: "dev.local"}, dir, caddy)
	if err := p.Apply(context.Background(), discovery.Update{}); err != nil {
		t.Fatal(err)
	}
	if len(caddy.routes) != 1 || caddy.routes[indexRouteID] == nil {
		t.Fatalf("index route not registered: %v", caddy.routes)
	}
	if len(caddy.policies) != 1 || caddy.policies[tlsPolicyID] == nil {
		t.Fatalf("index TLS policy not registered: %v", caddy.policies)
	}
	var route struct {
		Handle []any `json:"handle"`
		Match  []any `json:"match"`
	}
	if err := json.Unmarshal(caddy.routes[indexRouteID], &route); err != nil {
		t.Fatal(err)
	}
	root := route.Handle[0].(map[string]any)["routes"].([]any)[0].(map[string]any)["handle"].([]any)[0].(map[string]any)["root"]
	if root != dir {
		t.Errorf("index root = %v, want %v", root, dir)
	}
	hosts := route.Match[0].(map[string]any)["host"].([]any)
	if !slices.Equal(hosts, []any{"dev.local", "dev.localhost"}) {
		t.Errorf("index hosts = %v", hosts)
	}
	var policy map[string]any
	if err := json.Unmarshal(caddy.policies[tlsPolicyID], &policy); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(policy["subjects"].([]any), []any{"dev.local", "dev.localhost"}) {
		t.Errorf("TLS subjects = %v", policy["subjects"])
	}
}

func TestApplySkipsIndexHostClaimedByContainer(t *testing.T) {
	caddy := &fakeCaddy{}
	p := New(&config.Config{TLD: "dev.local"}, t.TempDir(), caddy)
	update := discovery.Update{Snapshot: []*discovery.ContainerInfo{{
		ContainerID: "a", ContainerName: "custom", IsRunning: true,
		CustomDomains: []discovery.CustomDomain{{Port: 8080, Domain: "dev.local"}},
	}}}
	if err := p.Apply(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	var route struct {
		Match []any `json:"match"`
	}
	if err := json.Unmarshal(caddy.routes[indexRouteID], &route); err != nil {
		t.Fatal(err)
	}
	hosts := route.Match[0].(map[string]any)["host"].([]any)
	if !slices.Equal(hosts, []any{"dev.localhost"}) {
		t.Errorf("index hosts = %v, want dev.localhost only", hosts)
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
