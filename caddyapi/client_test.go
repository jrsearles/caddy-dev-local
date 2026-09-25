package caddyapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

type apiState struct {
	mu            sync.Mutex
	serverExists  bool
	policiesExist bool
	routes        []json.RawMessage
	policies      []json.RawMessage
	config        string
	httpPort      int
	httpsPort     int
	listen        []string
	patched       []string
	posted        []string
	deleted       []string
	policyWrites  int
	missingStatus int
}

func (s *apiState) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	write := func(value any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(value)
	}
	missing := func() {
		status := s.missingStatus
		if status == 0 {
			status = http.StatusNotFound
		}
		message := "server missing"
		if status == http.StatusBadRequest {
			message = "invalid traversal path at: config/apps/http/servers/srv0"
		}
		http.Error(w, message, status)
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/config/":
		_, _ = w.Write([]byte(s.config))
	case r.Method == http.MethodGet && r.URL.Path == "/config/apps/http/servers/srv0/routes":
		if !s.serverExists {
			missing()
			return
		}
		write(s.routes)
	case r.Method == http.MethodGet && r.URL.Path == "/config/apps/http":
		write(map[string]any{"http_port": s.httpPort, "https_port": s.httpsPort})
	case r.Method == http.MethodGet && r.URL.Path == "/config/apps/http/servers/srv0":
		if !s.serverExists {
			missing()
			return
		}
		write(map[string]any{"routes": s.routes})
	case r.Method == http.MethodPut && r.URL.Path == "/config/apps/http/servers/srv0":
		s.serverExists = true
		var server struct {
			Routes []json.RawMessage `json:"routes"`
			Listen []string          `json:"listen"`
		}
		_ = json.NewDecoder(r.Body).Decode(&server)
		s.routes = server.Routes
		s.listen = server.Listen
		write(map[string]any{})
	case r.Method == http.MethodPost && r.URL.Path == "/config/apps/http/servers/srv0/routes/-":
		var resource json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&resource)
		s.routes = append(s.routes, resource)
		id, _ := resourceID(resource)
		s.posted = append(s.posted, id)
		write(map[string]any{})
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/id/"):
		id := strings.TrimPrefix(r.URL.Path, "/id/")
		var resource json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&resource)
		for i, route := range s.routes {
			if routeID, _ := resourceID(route); routeID == id {
				s.routes[i] = resource
				s.patched = append(s.patched, id)
				write(map[string]any{})
				return
			}
		}
		http.Error(w, "id missing", http.StatusNotFound)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/id/"):
		id := strings.TrimPrefix(r.URL.Path, "/id/")
		for i, route := range s.routes {
			if routeID, _ := resourceID(route); routeID == id {
				s.routes = slices.Delete(s.routes, i, i+1)
				break
			}
		}
		s.deleted = append(s.deleted, id)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && r.URL.Path == "/config/apps/tls/automation/policies":
		if !s.policiesExist {
			missing()
			return
		}
		write(s.policies)
	case (r.Method == http.MethodPatch || r.Method == http.MethodPut) && r.URL.Path == "/config/apps/tls/automation/policies":
		s.policiesExist = true
		s.policyWrites++
		var decoded []json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&decoded)
		s.policies = decoded
		write(map[string]any{})
	default:
		http.Error(w, fmt.Sprintf("unexpected %s %s", r.Method, r.URL.Path), http.StatusNotFound)
	}
}

func raw(value string) json.RawMessage { return json.RawMessage(value) }

const tlsPolicyID = "devlocal-tls"

func managed(id string) bool { return strings.HasPrefix(id, "devlocal-") }

func TestReconcileAdoptsActualStateAndPreservesUnrelatedResources(t *testing.T) {
	state := &apiState{
		serverExists:  true,
		policiesExist: true,
		routes: []json.RawMessage{
			raw(`{"@id":"user-route","value":"keep"}`),
			raw(`{"@id":"devlocal-existing","value":"old"}`),
			raw(`{"@id":"devlocal-orphan"}`),
		},
		policies: []json.RawMessage{
			raw(`{"@id":"user-policy","value":"keep"}`),
			raw(`{"@id":"devlocal-tls","subjects":["old"]}`),
		},
	}
	server := httptest.NewServer(http.HandlerFunc(state.handler))
	defer server.Close()
	client := New(Options{BaseURL: server.URL})

	err := client.Reconcile(context.Background(), map[string]json.RawMessage{
		"devlocal-existing": raw(`{"@id":"devlocal-existing","value":"new"}`),
		"devlocal-new":      raw(`{"@id":"devlocal-new"}`),
	}, map[string]json.RawMessage{
		"devlocal-tls": raw(`{"@id":"devlocal-tls","subjects":["new"]}`),
	}, managed)
	if err != nil {
		t.Fatal(err)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if !slices.Contains(state.patched, "devlocal-existing") {
		t.Errorf("existing route was not adopted via PATCH: %v", state.patched)
	}
	if !slices.Contains(state.posted, "devlocal-new") {
		t.Errorf("missing route was not appended: %v", state.posted)
	}
	if !slices.Contains(state.deleted, "devlocal-orphan") {
		t.Errorf("orphan route was not deleted: %v", state.deleted)
	}
	if _, ok := resourcesByID(state.routes, func(id string) bool { return id == "user-route" })["user-route"]; !ok {
		t.Error("unrelated route was removed")
	}
	if id, _ := resourceID(state.policies[0]); id != tlsPolicyID {
		t.Errorf("first policy ID = %q, want %q", id, tlsPolicyID)
	}
	if id, _ := resourceID(state.policies[1]); id != "user-policy" {
		t.Errorf("unrelated policy was not preserved: %q", id)
	}
}

func TestReconcileMissingServerPolicy(t *testing.T) {
	state := &apiState{}
	server := httptest.NewServer(http.HandlerFunc(state.handler))
	defer server.Close()
	err := New(Options{BaseURL: server.URL}).Reconcile(context.Background(), nil, nil, managed)
	if err == nil || !strings.Contains(err.Error(), "creation is disabled") || !strings.Contains(err.Error(), "server missing") {
		t.Fatalf("error = %v, want disabled policy and response body", err)
	}
}

func TestReconcileCreatesMissingServerForInvalidTraversal(t *testing.T) {
	state := &apiState{httpPort: 9080, httpsPort: 9443, missingStatus: http.StatusBadRequest}
	server := httptest.NewServer(http.HandlerFunc(state.handler))
	defer server.Close()
	client := New(Options{BaseURL: server.URL, AllowCreateServer: true, HTTPPort: 8080, HTTPSPort: 8443})
	if err := client.Reconcile(context.Background(), map[string]json.RawMessage{
		"devlocal-new": raw(`{"@id":"devlocal-new"}`),
	}, nil, managed); err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.serverExists || !slices.Contains(state.posted, "devlocal-new") {
		t.Fatalf("serverExists=%v posted=%v", state.serverExists, state.posted)
	}
	if !slices.Equal(state.listen, []string{":9443", ":9080"}) {
		t.Fatalf("listen=%v, want [:9443 :9080]", state.listen)
	}
}

func TestRunningConfig(t *testing.T) {
	state := &apiState{config: `{"apps":{"http":{}}}`}
	server := httptest.NewServer(http.HandlerFunc(state.handler))
	defer server.Close()
	got, err := New(Options{BaseURL: server.URL}).RunningConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != state.config {
		t.Fatalf("RunningConfig() = %q, want %q", got, state.config)
	}
}

func TestReconcileDoesNotRewriteMatchingState(t *testing.T) {
	route := raw(`{"@id":"devlocal-existing","value":"same"}`)
	policy := raw(`{"@id":"devlocal-tls","subjects":["same"]}`)
	state := &apiState{
		serverExists: true, policiesExist: true,
		routes: []json.RawMessage{route}, policies: []json.RawMessage{policy},
	}
	server := httptest.NewServer(http.HandlerFunc(state.handler))
	defer server.Close()
	client := New(Options{BaseURL: server.URL})
	if err := client.Reconcile(context.Background(), map[string]json.RawMessage{"devlocal-existing": route}, map[string]json.RawMessage{"devlocal-tls": policy}, managed); err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.patched) != 0 || len(state.posted) != 0 || len(state.deleted) != 0 || state.policyWrites != 0 {
		t.Fatalf("matching state was rewritten: patched=%v posted=%v deleted=%v policyWrites=%d", state.patched, state.posted, state.deleted, state.policyWrites)
	}
}

func TestReconcilePoliciesStableOrderAcrossOwners(t *testing.T) {
	caddyOwned := func(id string) bool {
		return strings.HasPrefix(id, "devlocal-route-") || id == "devlocal-tls"
	}
	uiOwned := func(id string) bool {
		return id == "devlocal-index" || id == "devlocal-tls-ui"
	}
	caddy := raw(`{"@id":"devlocal-tls","subjects":["web.dev.local"]}`)
	ui := raw(`{"@id":"devlocal-tls-ui","subjects":["dev.local"]}`)
	user := raw(`{"@id":"user-policy"}`)
	state := &apiState{
		serverExists: true, policiesExist: true,
		policies: []json.RawMessage{ui, user},
	}
	server := httptest.NewServer(http.HandlerFunc(state.handler))
	defer server.Close()
	client := New(Options{BaseURL: server.URL})

	if err := client.Reconcile(context.Background(), nil, map[string]json.RawMessage{"devlocal-tls": caddy}, caddyOwned); err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	if writes := state.policyWrites; writes != 1 {
		t.Fatalf("policyWrites after caddy reconcile = %d, want 1", writes)
	}
	if got := policyIDList(state.policies); !slices.Equal(got, []string{"devlocal-tls", "devlocal-tls-ui", "user-policy"}) {
		t.Errorf("order after caddy reconcile = %v", got)
	}
	state.mu.Unlock()

	if err := client.Reconcile(context.Background(), nil, map[string]json.RawMessage{"devlocal-tls-ui": ui}, uiOwned); err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if got := policyIDList(state.policies); !slices.Equal(got, []string{"devlocal-tls", "devlocal-tls-ui", "user-policy"}) {
		t.Errorf("order after ui reconcile = %v", got)
	}
	if writes := state.policyWrites; writes != 1 {
		t.Fatalf("policyWrites after ui reconcile = %d, want no rewrite", writes)
	}
}

func policyIDList(policies []json.RawMessage) []string {
	ids := make([]string, 0, len(policies))
	for _, policy := range policies {
		id, _ := resourceID(policy)
		ids = append(ids, id)
	}
	return ids
}

func TestCleanupRemovesOnlyOwnedResources(t *testing.T) {
	state := &apiState{
		serverExists: true, policiesExist: true,
		routes: []json.RawMessage{
			raw(`{"@id":"user-route"}`),
			raw(`{"@id":"devlocal-route-web"}`),
			raw(`{"@id":"devlocal-index"}`),
		},
		policies: []json.RawMessage{
			raw(`{"@id":"devlocal-tls"}`),
			raw(`{"@id":"devlocal-tls-ui"}`),
			raw(`{"@id":"user-policy"}`),
		},
	}
	server := httptest.NewServer(http.HandlerFunc(state.handler))
	defer server.Close()
	if err := New(Options{BaseURL: server.URL}).Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.routes) != 1 {
		t.Fatalf("routes after cleanup = %s", state.routes)
	}
	if id, _ := resourceID(state.routes[0]); id != "user-route" {
		t.Fatalf("remaining route = %q", id)
	}
	if len(state.policies) != 1 {
		t.Fatalf("policies after cleanup = %s", state.policies)
	}
	if id, _ := resourceID(state.policies[0]); id != "user-policy" {
		t.Fatalf("remaining policy = %q", id)
	}
}

func TestCleanupIgnoresMissingResources(t *testing.T) {
	state := &apiState{missingStatus: http.StatusBadRequest}
	server := httptest.NewServer(http.HandlerFunc(state.handler))
	defer server.Close()
	if err := New(Options{BaseURL: server.URL, AllowCreateServer: true}).Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.serverExists {
		t.Fatal("cleanup created a missing server")
	}
}
