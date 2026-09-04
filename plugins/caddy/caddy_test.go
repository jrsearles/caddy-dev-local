package caddy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jrsearles/caddy-dev-local/caddyapi"
	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
)

type capturedAPI struct {
	mu       sync.Mutex
	routes   []json.RawMessage
	policies []json.RawMessage
}

func (a *capturedAPI) serveHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/routes"):
		_ = json.NewEncoder(w).Encode(a.routes)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/routes/-"):
		var route json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&route)
		a.routes = append(a.routes, route)
		_, _ = w.Write([]byte(`{}`))
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/policies"):
		_ = json.NewEncoder(w).Encode(a.policies)
	case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/policies"):
		_ = json.NewDecoder(r.Body).Decode(&a.policies)
		_, _ = w.Write([]byte(`{}`))
	default:
		http.Error(w, "unexpected request", http.StatusNotFound)
	}
}

func TestPluginBuildsRouteIndexAndTLSJSON(t *testing.T) {
	api := &capturedAPI{}
	server := httptest.NewServer(http.HandlerFunc(api.serveHTTP))
	defer server.Close()
	cfg := &config.Config{TLD: "dev.local", Tracing: true}
	plugin := New(cfg, "/tmp/devlocal-index", caddyapi.New(caddyapi.Options{BaseURL: server.URL}))
	delta := discovery.Delta{Snapshot: []*discovery.ContainerInfo{{
		ContainerName: "web", IsRunning: true, SelectedPort: 8080,
	}}}
	if err := plugin.Apply(context.Background(), delta); err != nil {
		t.Fatal(err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	objects := make(map[string]map[string]any)
	for _, raw := range api.routes {
		var object map[string]any
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatal(err)
		}
		objects[object["@id"].(string)] = object
	}
	route := objects["devlocal-route-web-dev-local"]
	if route == nil {
		t.Fatalf("container route missing from %v", objects)
	}
	handles := route["handle"].([]any)
	if handles[0].(map[string]any)["handler"] != "tracing" {
		t.Errorf("first handler = %v, want tracing", handles[0])
	}
	subroute := handles[1].(map[string]any)
	inner := subroute["routes"].([]any)[0].(map[string]any)
	proxy := inner["handle"].([]any)[0].(map[string]any)
	dial := proxy["upstreams"].([]any)[0].(map[string]any)["dial"]
	if dial != "localhost:8080" {
		t.Errorf("upstream dial = %v, want localhost:8080", dial)
	}
	index := objects[indexRouteID]
	if index == nil {
		t.Fatal("stable index route missing")
	}
	indexHandle := index["handle"].([]any)[0].(map[string]any)
	indexInner := indexHandle["routes"].([]any)[0].(map[string]any)
	vars := indexInner["handle"].([]any)[0].(map[string]any)
	if vars["root"] != "/tmp/devlocal-index" {
		t.Errorf("index root = %v", vars["root"])
	}
	indexHosts := index["match"].([]any)[0].(map[string]any)["host"].([]any)
	if !slices.Equal(indexHosts, []any{"dev.local", "dev.localhost"}) {
		t.Errorf("index hosts = %v", indexHosts)
	}
	if len(api.policies) != 1 {
		t.Fatalf("policies = %s", api.policies)
	}
	var policy map[string]any
	if err := json.Unmarshal(api.policies[0], &policy); err != nil {
		t.Fatal(err)
	}
	if policy["@id"] != tlsPolicyID || policy["issuers"].([]any)[0].(map[string]any)["module"] != "internal" {
		t.Errorf("TLS policy = %v", policy)
	}
}

func TestBuildConfigWithoutIndexDirectory(t *testing.T) {
	routes, _, err := buildConfig(&config.Config{TLD: "dev.local"}, "", map[string][]string{
		"web.dev.local": {"localhost:8080"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := routes[indexRouteID]; ok {
		t.Fatal("index route present when UI is disabled")
	}
}
