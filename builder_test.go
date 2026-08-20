package caddydevlocal

import (
	"encoding/json"
	"testing"
)

func TestDevlocalRouteID(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"my-nginx.dev.local", "devlocal-route-my-nginx-dev-local"},
		{"myapp.web.dev.local", "devlocal-route-myapp-web-dev-local"},
		{"api.custom.local", "devlocal-route-api-custom-local"},
	}
	for _, tt := range tests {
		if got := devlocalRouteID(tt.host); got != tt.want {
			t.Errorf("devlocalRouteID(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestBuildDevlocalConfig(t *testing.T) {
	got, err := buildDevlocalConfig("dev.local", "/home/user/.cache/caddy-dev-local", map[string][]string{
		"myapp.web.dev.local": {"web:3000", "web:3001"},
		"my-nginx.dev.local":  {"nginx:80"},
		"api.custom.local":    {"web:8080"},
	}, true)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.routes) != 3 {
		t.Fatalf("expected 3 routes, got %d", len(got.routes))
	}
	for _, id := range []string{
		"devlocal-route-myapp-web-dev-local",
		"devlocal-route-my-nginx-dev-local",
		"devlocal-route-api-custom-local",
	} {
		if _, ok := got.routes[id]; !ok {
			t.Errorf("missing route %s", id)
		}
	}
	if _, ok := got.policies[devlocalTLSPolicyID]; !ok {
		t.Error("expected merged TLS policy in policies map")
	}
	if len(got.indexRoute) == 0 {
		t.Error("expected index route")
	}
}

func TestBuildDevlocalConfigEmptyTargets(t *testing.T) {
	got, err := buildDevlocalConfig("dev.local", "/cache", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.routes) != 0 {
		t.Errorf("expected no container routes, got %d", len(got.routes))
	}
	if len(got.policies) != 0 {
		t.Errorf("expected no TLS policies, got %d", len(got.policies))
	}
	if len(got.indexRoute) == 0 {
		t.Error("expected index route even with no containers")
	}
}

func TestBuildIndexRouteHosts(t *testing.T) {
	route, err := buildIndexRoute("dev.local", "/cache", nil)
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]any
	if err := json.Unmarshal(route, &obj); err != nil {
		t.Fatal(err)
	}

	match := obj["match"].([]any)
	hosts := match[0].(map[string]any)["host"].([]any)
	if len(hosts) != 1 || hosts[0] != "dev.localhost" {
		t.Errorf("index route hosts = %v, want [dev.localhost]", hosts)
	}
}

func TestBuildIndexRouteSkipsAliasCollision(t *testing.T) {
	route, err := buildIndexRoute("dev.local", "/cache", []string{"dev.dev.local", "dev.localhost"})
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]any
	if err := json.Unmarshal(route, &obj); err != nil {
		t.Fatal(err)
	}

	match := obj["match"].([]any)
	hostsRaw := match[0].(map[string]any)["host"]
	if hostsRaw != nil {
		hosts := hostsRaw.([]any)
		if len(hosts) != 0 {
			t.Errorf("index route hosts = %v, want []", hosts)
		}
	}
}

func TestBuildIndexRouteNoDupWhenTldIsLocalhost(t *testing.T) {
	route, err := buildIndexRoute("localhost", "/cache", nil)
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]any
	if err := json.Unmarshal(route, &obj); err != nil {
		t.Fatal(err)
	}

	match := obj["match"].([]any)
	hosts := match[0].(map[string]any)["host"].([]any)
	if len(hosts) != 1 || hosts[0] != "localhost" {
		t.Errorf("index route hosts = %v, want [localhost]", hosts)
	}
}

func TestBuildReverseProxyRoute(t *testing.T) {
	route, err := buildReverseProxyRoute("my-nginx.dev.local", []string{"nginx:80", "nginx:81"}, true)
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]any
	if err := json.Unmarshal(route, &obj); err != nil {
		t.Fatal(err)
	}

	if obj["@id"] != devlocalRouteID("my-nginx.dev.local") {
		t.Errorf("@id = %v, want %v", obj["@id"], devlocalRouteID("my-nginx.dev.local"))
	}
	if obj["terminal"] != true {
		t.Errorf("terminal = %v, want true", obj["terminal"])
	}

	match := obj["match"].([]any)
	hosts := match[0].(map[string]any)["host"].([]any)
	if len(hosts) != 1 || hosts[0] != "my-nginx.dev.local" {
		t.Errorf("match hosts = %v, want [my-nginx.dev.local]", hosts)
	}

	handle := obj["handle"].([]any)
	subroute := handle[1].(map[string]any)
	routes := subroute["routes"].([]any)
	inner := routes[0].(map[string]any)
	innerHandle := inner["handle"].([]any)
	proxy := innerHandle[0].(map[string]any)
	if proxy["handler"] != "reverse_proxy" {
		t.Errorf("handler = %v, want reverse_proxy", proxy["handler"])
	}
	upstreams := proxy["upstreams"].([]any)
	if len(upstreams) != 2 {
		t.Fatalf("expected 2 upstreams, got %d", len(upstreams))
	}
	if upstreams[0].(map[string]any)["dial"] != "nginx:80" {
		t.Errorf("upstream[0] = %v, want nginx:80", upstreams[0])
	}
	if upstreams[1].(map[string]any)["dial"] != "nginx:81" {
		t.Errorf("upstream[1] = %v, want nginx:81", upstreams[1])
	}
}

func TestBuildTLSPolicy(t *testing.T) {
	policy, err := buildTLSPolicy([]string{"my-nginx.dev.local", "api.custom.local"})
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]any
	if err := json.Unmarshal(policy, &obj); err != nil {
		t.Fatal(err)
	}

	if obj["@id"] != devlocalTLSPolicyID {
		t.Errorf("@id = %v, want %v", obj["@id"], devlocalTLSPolicyID)
	}
	issuers := obj["issuers"].([]any)
	if len(issuers) != 1 || issuers[0].(map[string]any)["module"] != "internal" {
		t.Errorf("issuers = %v, want [{module: internal}]", issuers)
	}
	subjects := obj["subjects"].([]any)
	if len(subjects) != 2 || subjects[0] != "api.custom.local" || subjects[1] != "my-nginx.dev.local" {
		t.Errorf("subjects = %v, want sorted [api.custom.local my-nginx.dev.local]", subjects)
	}
}

func TestBuildReverseProxyRouteWithTracing(t *testing.T) {
	route, err := buildReverseProxyRoute("my-nginx.dev.local", []string{"nginx:80"}, true)
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]any
	if err := json.Unmarshal(route, &obj); err != nil {
		t.Fatal(err)
	}

	handle := obj["handle"].([]any)
	if handle[0].(map[string]any)["handler"] != "tracing" {
		t.Errorf("first handler = %v, want tracing", handle[0].(map[string]any)["handler"])
	}
	subroute := handle[1].(map[string]any)
	routes := subroute["routes"].([]any)
	inner := routes[0].(map[string]any)
	innerHandle := inner["handle"].([]any)
	proxy := innerHandle[0].(map[string]any)
	if proxy["handler"] != "reverse_proxy" {
		t.Errorf("second handler = %v, want reverse_proxy", proxy["handler"])
	}
}

func TestBuildReverseProxyRouteWithoutTracing(t *testing.T) {
	route, err := buildReverseProxyRoute("my-nginx.dev.local", []string{"nginx:80"}, false)
	if err != nil {
		t.Fatal(err)
	}

	var obj map[string]any
	if err := json.Unmarshal(route, &obj); err != nil {
		t.Fatal(err)
	}

	handle := obj["handle"].([]any)
	if len(handle) != 1 {
		t.Fatalf("expected 1 handle entry, got %d", len(handle))
	}
	subroute := handle[0].(map[string]any)
	if subroute["handler"] != "subroute" {
		t.Errorf("handler = %v, want subroute", subroute["handler"])
	}
}
