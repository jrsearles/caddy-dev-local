package generator

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
)

func info(project, service, cname string) *discovery.ContainerInfo {
	return &discovery.ContainerInfo{
		Project:       project,
		Service:       service,
		ContainerName: cname,
		IsCompose:     project != "" && service != "",
	}
}

func TestDomainComputation(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local"}

	tests := []struct {
		name     string
		project  string
		service  string
		cName    string
		expected string
	}{
		{"compose service", "myapp", "web", "web", "myapp.web.dev.local"},
		{"compose api", "myapp", "api", "api", "myapp.api.dev.local"},
		{"standalone", "", "", "my-nginx", "my-nginx.dev.local"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domainForContainer(cfg, info(tt.project, tt.service, tt.cName))
			if got != tt.expected {
				t.Errorf("domainForContainer() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDomainComputationLocalhost(t *testing.T) {
	tests := []struct {
		name     string
		project  string
		service  string
		cName    string
		expected string
	}{
		{"compose service", "myapp", "web", "web", "myapp.web.localhost"},
		{"compose api", "myapp", "api", "api", "myapp.api.localhost"},
		{"standalone", "", "", "my-nginx", "my-nginx.localhost"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domainForContainerLocalhost(info(tt.project, tt.service, tt.cName))
			if got != tt.expected {
				t.Errorf("domainForContainerLocalhost() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestProbeHTTPPort(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	_, portStr, _ := net.SplitHostPort(ts.Listener.Addr().String())
	var port uint16
	fmt.Sscanf(portStr, "%d", &port) //nolint:errcheck // test helper, value validated below

	got, err := ProbeHTTPPort("localhost", []uint16{port}, 2*time.Second)
	if err != nil {
		t.Fatalf("ProbeHTTPPort() error = %v", err)
	}
	if got != port {
		t.Errorf("ProbeHTTPPort() = %v, want %v", got, port)
	}
}

func TestProbeHTTPPortNoHTTP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		conn.Write([]byte("not http")) //nolint:errcheck // test helper
		conn.Close()
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port uint16
	fmt.Sscanf(portStr, "%d", &port) //nolint:errcheck // test helper, value validated below

	_, err = ProbeHTTPPort("localhost", []uint16{port}, 500*time.Millisecond)
	if err == nil {
		t.Fatal("ProbeHTTPPort() should have returned error for non-HTTP server")
	}
}

func TestPortSelectorSelectsPort(t *testing.T) {
	cfg := &config.Config{ProbeTimeout: time.Second}
	calls := 0
	sel := PortSelector(cfg, func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		calls++
		if host != "localhost" {
			t.Errorf("host = %q, want localhost", host)
		}
		return ports[0], nil
	})

	i := &discovery.ContainerInfo{
		IsRunning:     true,
		ContainerName: "web",
		Ports:         []uint16{80, 8080},
	}
	sel(i)
	if i.SelectedPort != 80 {
		t.Errorf("SelectedPort = %d, want 80", i.SelectedPort)
	}
	if calls != 1 {
		t.Errorf("probe called %d times, want 1", calls)
	}
}

func TestPortSelectorSkipsStopped(t *testing.T) {
	cfg := &config.Config{ProbeTimeout: time.Second}
	calls := 0
	sel := PortSelector(cfg, func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		calls++
		return ports[0], nil
	})
	i := &discovery.ContainerInfo{IsRunning: false, Ports: []uint16{80}}
	sel(i)
	if calls != 0 {
		t.Errorf("probe called %d times, want 0 for stopped container", calls)
	}
}

func TestPortSelectorSkipsCustomDomains(t *testing.T) {
	cfg := &config.Config{ProbeTimeout: time.Second}
	calls := 0
	sel := PortSelector(cfg, func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		calls++
		return ports[0], nil
	})
	i := &discovery.ContainerInfo{
		IsRunning:     true,
		Ports:         []uint16{80},
		Labels:        map[string]string{"dev.local.domains": "80:api.custom.local"},
		CustomDomains: []discovery.CustomDomain{{Port: 80, Domain: "api.custom.local"}},
	}
	sel(i)
	if calls != 0 {
		t.Errorf("probe called %d times, want 0 for custom-domain container", calls)
	}
}

func TestEffectivePort(t *testing.T) {
	info := &discovery.ContainerInfo{
		PublishedPorts: map[uint16]uint16{80: 8080},
	}
	if got := effectivePort(info, 80); got != 8080 {
		t.Errorf("effectivePort(published, 80) = %d, want 8080", got)
	}
	info = &discovery.ContainerInfo{}
	if got := effectivePort(info, 80); got != 80 {
		t.Errorf("effectivePort(unpublished, 80) = %d, want 80", got)
	}
}

func TestDomainsComposeAndStandalone(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local"}
	containers := []*discovery.ContainerInfo{
		{
			ContainerName: "web", Project: "myapp", Service: "web", IsCompose: true,
			IsRunning: true, Ports: []uint16{3000}, SelectedPort: 3000,
		},
		{
			ContainerName: "nginx", IsRunning: true, Ports: []uint16{80}, SelectedPort: 80,
		},
	}

	domains := Domains(cfg, containers)
	expected := []string{"myapp.web.dev.local", "myapp.web.localhost", "nginx.dev.local", "nginx.localhost"}
	if !slices.Equal(domains, expected) {
		t.Fatalf("expected domains %v, got %v", expected, domains)
	}
}

func TestDomainsIncludesLocalhost(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local"}
	containers := []*discovery.ContainerInfo{
		{
			ContainerName: "nginx", IsRunning: true,
			Ports: []uint16{80}, PublishedPorts: map[uint16]uint16{80: 8080}, SelectedPort: 8080,
		},
	}

	domains := Domains(cfg, containers)
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d: %v", len(domains), domains)
	}
	if domains[0] != "nginx.dev.local" || domains[1] != "nginx.localhost" {
		t.Errorf("unexpected domains: %v", domains)
	}
}

func TestDomainsCustomDomains(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local"}
	containers := []*discovery.ContainerInfo{
		{
			ContainerName: "web", IsCompose: true, Project: "myapp", Service: "web",
			IsRunning:     true,
			CustomDomains: []discovery.CustomDomain{{Port: 3000, Domain: "api.custom.local"}, {Port: 3000, Domain: "admin.custom.local"}},
		},
	}

	domains := Domains(cfg, containers)
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d: %v", len(domains), domains)
	}
	if domains[0] != "admin.custom.local" || domains[1] != "api.custom.local" {
		t.Errorf("unexpected domains: %v", domains)
	}
}

func TestDomainsExcludesStopped(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local"}
	containers := []*discovery.ContainerInfo{
		{ContainerName: "web", Project: "myapp", Service: "web", IsCompose: true, IsRunning: true, Ports: []uint16{3000}, SelectedPort: 3000},
		{ContainerName: "api", Project: "myapp", Service: "api", IsCompose: true, IsRunning: false, Ports: []uint16{3000}},
	}

	domains := Domains(cfg, containers)
	expected := []string{"myapp.web.dev.local", "myapp.web.localhost"}
	if !slices.Equal(domains, expected) {
		t.Fatalf("expected domains %v, got %v", expected, domains)
	}
}

func TestDomainsSorted(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local"}
	containers := []*discovery.ContainerInfo{
		{ContainerName: "zebra", IsRunning: true, Ports: []uint16{80}, SelectedPort: 80},
		{ContainerName: "alpha", IsRunning: true, Ports: []uint16{80}, SelectedPort: 80},
		{ContainerName: "middle", IsRunning: true, Ports: []uint16{80}, SelectedPort: 80},
	}

	domains := Domains(cfg, containers)
	expected := []string{"alpha.dev.local", "alpha.localhost", "middle.dev.local", "middle.localhost", "zebra.dev.local", "zebra.localhost"}
	if !slices.Equal(domains, expected) {
		t.Fatalf("expected %d domains, got %d: %v", len(expected), len(domains), domains)
	}
}

func TestDomainsIncludesPortsWithPublished(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local"}
	containers := []*discovery.ContainerInfo{
		{ContainerName: "nginx", IsRunning: true, Ports: []uint16{80}, PublishedPorts: map[uint16]uint16{80: 8080}},
	}

	domains := Domains(cfg, containers)
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d: %v", len(domains), domains)
	}
	if domains[0] != "nginx.dev.local" || domains[1] != "nginx.localhost" {
		t.Errorf("unexpected domains: %v", domains)
	}
}

func TestDomainsExcludesUnpublished(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local"}
	containers := []*discovery.ContainerInfo{
		{ContainerName: "internal", IsRunning: true, Ports: []uint16{80}},
	}

	domains := Domains(cfg, containers)
	if len(domains) != 0 {
		t.Errorf("expected 0 domains for unpublished container, got %d: %v", len(domains), domains)
	}
}

func TestDomainsExcludesPortless(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local"}
	containers := []*discovery.ContainerInfo{
		{ContainerName: "web", Project: "myapp", Service: "web", IsCompose: true, IsRunning: true},
	}

	domains := Domains(cfg, containers)
	if len(domains) != 0 {
		t.Errorf("expected 0 domains for portless container, got %d: %v", len(domains), domains)
	}
}

func TestDomainTargetsMergesDuplicateDomains(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local"}
	containers := []*discovery.ContainerInfo{
		{ContainerName: "web", Project: "myapp", Service: "web", IsCompose: true, IsRunning: true, Ports: []uint16{3000}, SelectedPort: 3000},
		{ContainerName: "web", Project: "myapp", Service: "web", IsCompose: true, IsRunning: true, Ports: []uint16{3001}, SelectedPort: 3001},
	}

	targets := DomainTargets(cfg, containers)
	if len(targets) != 2 {
		t.Fatalf("expected 2 merged domains, got %d: %v", len(targets), targets)
	}
	want := []string{"localhost:3000", "localhost:3001"}
	if !slices.Equal(targets["myapp.web.dev.local"], want) {
		t.Errorf("DomainTargets() = %v, want %v", targets["myapp.web.dev.local"], want)
	}
	if !slices.Equal(targets["myapp.web.localhost"], want) {
		t.Errorf("DomainTargets() = %v, want %v", targets["myapp.web.localhost"], want)
	}
}

func TestDomainTargetsStandalone(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local"}
	containers := []*discovery.ContainerInfo{
		{ContainerName: "nginx", IsRunning: true, Ports: []uint16{80}, PublishedPorts: map[uint16]uint16{80: 8080}, SelectedPort: 8080},
	}

	targets := DomainTargets(cfg, containers)
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets (tld + localhost), got %d: %v", len(targets), targets)
	}
	if !slices.Equal(targets["nginx.dev.local"], []string{"localhost:8080"}) {
		t.Errorf("unexpected tld target: %v", targets["nginx.dev.local"])
	}
	if !slices.Equal(targets["nginx.localhost"], []string{"localhost:8080"}) {
		t.Errorf("unexpected localhost target: %v", targets["nginx.localhost"])
	}
}

func TestDomainTargetsCustomDomains(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local"}
	containers := []*discovery.ContainerInfo{
		{
			ContainerName: "web", Project: "myapp", Service: "web", IsCompose: true,
			IsRunning:     true,
			CustomDomains: []discovery.CustomDomain{{Port: 3000, Domain: "api.custom.local"}, {Port: 8080, Domain: "admin.custom.local"}},
		},
	}

	targets := DomainTargets(cfg, containers)
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d: %v", len(targets), targets)
	}
	if _, ok := targets["api.custom.local"]; !ok {
		t.Error("expected custom domain api.custom.local in targets")
	}
	if _, ok := targets["admin.custom.local"]; !ok {
		t.Error("expected custom domain admin.custom.local in targets")
	}
	if _, ok := targets["myapp.web.dev.local"]; ok {
		t.Error("auto-generated domain should not appear when custom domains are set")
	}
}

func TestDomainTargetsCustomDomainsPublished(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local"}
	containers := []*discovery.ContainerInfo{
		{
			ContainerName: "web", Project: "myapp", Service: "web", IsCompose: true,
			PublishedPorts: map[uint16]uint16{80: 8080}, IsRunning: true,
			CustomDomains: []discovery.CustomDomain{{Port: 80, Domain: "api.custom.local"}},
		},
	}

	targets := DomainTargets(cfg, containers)
	if !slices.Equal(targets["api.custom.local"], []string{"localhost:8080"}) {
		t.Errorf("expected custom domain target localhost:8080, got %v", targets["api.custom.local"])
	}
	if _, ok := targets["myapp.web.dev.local"]; ok {
		t.Error("auto-generated domain should not appear when custom domains are set")
	}
}

func TestTLDLocalhost(t *testing.T) {
	tests := []struct {
		tld  string
		want string
	}{
		{"dev.local", "dev.localhost"},
		{"test.local", "test.localhost"},
		{"localhost", "localhost"},
		{"my.dev.local", "my.localhost"},
	}
	for _, tt := range tests {
		if got := TLDLocalhost(tt.tld); got != tt.want {
			t.Errorf("TLDLocalhost(%q) = %q, want %q", tt.tld, got, tt.want)
		}
	}
}

func TestGetLabel(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		key    string
		want   string
	}{
		{"nil labels", nil, "key", ""},
		{"missing key", map[string]string{}, "key", ""},
		{"present", map[string]string{"key": "value"}, "key", "value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getLabel(tt.labels, tt.key)
			if got != tt.want {
				t.Errorf("getLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}
