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

func TestHookBuildsRouteAndTLSJSON(t *testing.T) {
	api := &capturedAPI{}
	server := httptest.NewServer(http.HandlerFunc(api.serveHTTP))
	defer server.Close()
	cfg := &config.Config{TLD: "dev.local", Tracing: true}
	hook := New(cfg, caddyapi.New(caddyapi.Options{BaseURL: server.URL}))
	update := discovery.Update{Snapshot: []*discovery.ContainerInfo{{
		ContainerName: "web", IsRunning: true, SelectedPort: 8080,
	}}}
	if err := hook.Apply(context.Background(), update); err != nil {
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
	if _, ok := objects["devlocal-index"]; ok {
		t.Fatal("index route must be registered by the ui hook, not the caddy hook")
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
	subjects := policy["subjects"].([]any)
	if !slices.Equal(subjects, []any{"web.dev.local", "web.localhost"}) {
		t.Errorf("TLS policy subjects = %v, want container domains only", subjects)
	}
}

func TestBuildConfigProducesOnlyContainerRoutes(t *testing.T) {
	routes, policies, err := buildConfig(&config.Config{TLD: "dev.local"}, map[string][]string{
		"web.dev.local": {"localhost:8080"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 {
		t.Fatalf("routes = %v, want only the container route", routes)
	}
	var policy struct {
		ID       string   `json:"@id"`
		Subjects []string `json:"subjects"`
	}
	if err := json.Unmarshal(policies[tlsPolicyID], &policy); err != nil {
		t.Fatal(err)
	}
	if policy.ID != tlsPolicyID {
		t.Errorf("policy ID = %q, want %q", policy.ID, tlsPolicyID)
	}
	if !slices.Equal(policy.Subjects, []string{"web.dev.local"}) {
		t.Errorf("TLS policy subjects = %v", policy.Subjects)
	}
}
