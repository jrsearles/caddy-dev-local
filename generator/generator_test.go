package generator

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/jsearles/caddy-dev-local/config"
	"github.com/jsearles/caddy-dev-local/docker"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type mockDocker struct {
	containers []container.Summary
}

func (m *mockDocker) ContainerList(ctx context.Context, options client.ContainerListOptions) ([]container.Summary, error) {
	return m.containers, nil
}

func (m *mockDocker) ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error) {
	return container.InspectResponse{}, nil
}

func (m *mockDocker) Events(ctx context.Context, options client.EventsListOptions) (<-chan events.Message, <-chan error) {
	ch := make(chan events.Message)
	errCh := make(chan error)
	return ch, errCh
}

func (m *mockDocker) NetworkInspect(ctx context.Context, networkID string) (network.Inspect, error) {
	return network.Inspect{}, nil
}

func makeContainer(id, name, project, service string, ports []container.PortSummary, labels map[string]string, state container.ContainerState) container.Summary {
	if labels == nil {
		labels = make(map[string]string)
	}
	if project != "" {
		labels["com.docker.compose.project"] = project
	}
	if service != "" {
		labels["com.docker.compose.service"] = service
	}
	return container.Summary{
		ID:    id,
		Names: []string{"/" + name},
		Ports: ports,
		Labels: labels,
		State: state,
		NetworkSettings: &container.NetworkSettingsSummary{
			Networks: map[string]*network.EndpointSettings{
				"devlocal": {IPAddress: netip.MustParseAddr("172.18.0.2")},
			},
		},
	}
}

func TestDomainComputation(t *testing.T) {
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
	}

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
			info := &ContainerInfo{
				Project:       tt.project,
				Service:       tt.service,
				ContainerName: tt.cName,
				IsCompose:     tt.project != "" && tt.service != "",
			}
			gen := &Generator{cfg: cfg}
			got := gen.domainForContainer(info)
			if got != tt.expected {
				t.Errorf("domainForContainer() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDomainComputationLocalhost(t *testing.T) {
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
	}

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
			info := &ContainerInfo{
				Project:       tt.project,
				Service:       tt.service,
				ContainerName: tt.cName,
				IsCompose:     tt.project != "" && tt.service != "",
			}
			gen := &Generator{cfg: cfg}
			got := gen.domainForContainerLocalhost(info)
			if got != tt.expected {
				t.Errorf("domainForContainerLocalhost() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestShouldSkip(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{"no label", nil, false},
		{"empty label", map[string]string{"dev.local": ""}, false},
		{"false", map[string]string{"dev.local": "false"}, true},
		{"False", map[string]string{"dev.local": "False"}, true},
		{"0", map[string]string{"dev.local": "0"}, true},
		{"no", map[string]string{"dev.local": "no"}, true},
		{"true", map[string]string{"dev.local": "true"}, false},
		{"1", map[string]string{"dev.local": "1"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := container.Summary{Labels: tt.labels}
			got := shouldSkip(c)
			if got != tt.want {
				t.Errorf("shouldSkip() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractPorts(t *testing.T) {
	tests := []struct {
		name  string
		ports []container.PortSummary
		want  []uint16
	}{
		{
			"single port",
			[]container.PortSummary{{PrivatePort: 80, PublicPort: 0}},
			[]uint16{80},
		},
		{
			"multiple ports",
			[]container.PortSummary{
				{PrivatePort: 3000, PublicPort: 0},
				{PrivatePort: 8080, PublicPort: 0},
			},
			[]uint16{3000, 8080},
		},
		{
			"published ports",
			[]container.PortSummary{
				{PrivatePort: 80, PublicPort: 8080},
			},
			[]uint16{80},
		},
		{
			"no ports",
			[]container.PortSummary{},
			nil,
		},
		{
			"deduplicates",
			[]container.PortSummary{
				{PrivatePort: 80, PublicPort: 8080},
				{PrivatePort: 80, PublicPort: 0},
			},
			[]uint16{80},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := container.Summary{Ports: tt.ports}
			got := extractPorts(c)
			if len(got) != len(tt.want) {
				t.Errorf("extractPorts() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractPorts()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCustomDomains(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local"}
	gen := &Generator{cfg: cfg}

	tests := []struct {
		name  string
		label string
		want  []CustomDomain
	}{
		{
			"single domain",
			"3000:api.custom.local",
			[]CustomDomain{{Port: 3000, Domain: "api.custom.local"}},
		},
		{
			"multiple domains same port",
			"3000:api.custom.local;3000:api.alt.local",
			[]CustomDomain{
				{Port: 3000, Domain: "api.custom.local"},
				{Port: 3000, Domain: "api.alt.local"},
			},
		},
		{
			"multiple ports",
			"3000:api.custom.local;8080:admin.custom.local",
			[]CustomDomain{
				{Port: 3000, Domain: "api.custom.local"},
				{Port: 8080, Domain: "admin.custom.local"},
			},
		},
		{
			"empty",
			"",
			nil,
		},
		{
			"invalid no colon",
			"3000",
			nil,
		},
		{
			"invalid no domain",
			"3000:",
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &ContainerInfo{
				Labels: map[string]string{"dev.local.domains": tt.label},
			}
			got := gen.customDomains(info)
			if len(got) != len(tt.want) {
				t.Errorf("customDomains() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("customDomains()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestProbeHTTPPort(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == probeUserAgent {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	_, portStr, _ := net.SplitHostPort(ts.Listener.Addr().String())
	var port uint16
	fmt.Sscanf(portStr, "%d", &port)

	got, err := ProbeHTTPPort([]uint16{port}, 2*time.Second)
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
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Write([]byte("not http"))
		conn.Close()
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port uint16
	fmt.Sscanf(portStr, "%d", &port)

	_, err = ProbeHTTPPort([]uint16{port}, 500*time.Millisecond)
	if err == nil {
		t.Fatal("ProbeHTTPPort() should have returned error for non-HTTP server")
	}
}

func TestGeneratorRefresh(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "web", "myapp", "web",
			[]container.PortSummary{{PrivatePort: 3000, PublicPort: 0}},
			nil, "running"),
		makeContainer("c2", "api", "myapp", "api",
			[]container.PortSummary{
				{PrivatePort: 3000, PublicPort: 0},
				{PrivatePort: 8080, PublicPort: 0},
			},
			nil, "running"),
		makeContainer("c3", "ignored", "", "ignored",
			[]container.PortSummary{{PrivatePort: 6379, PublicPort: 0}},
			map[string]string{"dev.local": "false"}, "running"),
	}

	mock := &mockDocker{containers: containers}
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
		StaleTTL:       time.Hour,
	}

	gen := NewGenerator(cfg, mock)
	ctx := context.Background()

	if err := gen.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	infos := gen.Containers()
	if len(infos) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(infos))
	}

	for _, info := range infos {
		if info.ContainerName == "ignored" {
			t.Error("ignored container should not be in results")
		}
	}
}

func TestGeneratorCustomDomainsOverride(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "web", "myapp", "web",
			[]container.PortSummary{{PrivatePort: 3000, PublicPort: 0}},
			map[string]string{"dev.local.domains": "3000:api.custom.local"},
			"running"),
	}

	mock := &mockDocker{containers: containers}
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
		StaleTTL:       time.Hour,
	}

	gen := NewGenerator(cfg, mock)
	ctx := context.Background()

	if err := gen.Refresh(ctx); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	gen.SelectPorts(ctx)

	caddyfile := gen.GenerateCaddyfile()
	if len(caddyfile) == 0 {
		t.Fatal("expected non-empty Caddyfile")
	}

	if !contains(caddyfile, "api.custom.local") {
		t.Error("expected custom domain in Caddyfile")
	}
	if contains(caddyfile, "myapp.web.dev.local") {
		t.Error("auto-registered domain should not appear when custom domains are set")
	}
}

func TestGeneratorSinglePort(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "nginx", "", "",
			[]container.PortSummary{{PrivatePort: 80, PublicPort: 0}},
			nil, "running"),
	}

	mock := &mockDocker{containers: containers}
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
		StaleTTL:       time.Hour,
	}

	gen := NewGenerator(cfg, mock)
	ctx := context.Background()

	gen.Refresh(ctx)
	gen.SelectPorts(ctx)

	caddyfile := gen.GenerateCaddyfile()
	if !contains(caddyfile, "nginx.dev.local") {
		t.Error("expected nginx.dev.local in Caddyfile")
	}
	if !contains(caddyfile, "reverse_proxy nginx:80") {
		t.Error("expected reverse_proxy nginx:80 in Caddyfile")
	}
	if contains(caddyfile, "nginx.localhost") {
		t.Error("localhost domain should not appear in non-standalone mode")
	}
}

func TestStaleCleanup(t *testing.T) {
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
		StaleTTL:       time.Minute,
	}

	gen := &Generator{
		cfg:        cfg,
		containers: make(map[string]*ContainerInfo),
	}

	gen.containers["c1"] = &ContainerInfo{
		ContainerID:   "c1",
		ContainerName: "stale",
		IsRunning:     false,
		LastStopped:   time.Now().Add(-2 * time.Minute),
	}

	gen.containers["c2"] = &ContainerInfo{
		ContainerID:   "c2",
		ContainerName: "fresh",
		IsRunning:     false,
		LastStopped:   time.Now().Add(-30 * time.Second),
	}

	gen.StaleCleanup()

	if _, ok := gen.containers["c1"]; ok {
		t.Error("stale container should have been removed")
	}
	if _, ok := gen.containers["c2"]; !ok {
		t.Error("fresh stopped container should still exist")
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

func TestHasNetwork(t *testing.T) {
	c := container.Summary{
		NetworkSettings: &container.NetworkSettingsSummary{
			Networks: map[string]*network.EndpointSettings{
				"devlocal": {IPAddress: netip.MustParseAddr("172.18.0.2")},
				"other":    {IPAddress: netip.MustParseAddr("172.19.0.2")},
			},
		},
	}

	if !docker.HasNetwork(c, "devlocal") {
		t.Error("expected HasNetwork to return true for devlocal")
	}
	if docker.HasNetwork(c, "missing") {
		t.Error("expected HasNetwork to return false for missing")
	}
}

func TestContainerName(t *testing.T) {
	c := container.Summary{
		Names: []string{"/my-container"},
	}
	got := docker.ContainerName(c)
	if got != "my-container" {
		t.Errorf("ContainerName() = %q, want %q", got, "my-container")
	}

	c2 := container.Summary{
		Names: []string{},
		ID:    "abc123def456",
	}
	got2 := docker.ContainerName(c2)
	if got2 != "abc123def456" {
		t.Errorf("ContainerName() = %q, want %q", got2, "abc123def456")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestStandaloneCaddyfileGeneration(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "nginx", "", "",
			[]container.PortSummary{{PrivatePort: 80, PublicPort: 8080}},
			nil, "running"),
	}

	mock := &mockDocker{containers: containers}
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
		StaleTTL:       time.Hour,
		Standalone:     true,
	}

	gen := NewGenerator(cfg, mock)
	ctx := context.Background()

	gen.Refresh(ctx)
	gen.SelectPorts(ctx)

	caddyfile := gen.GenerateCaddyfile()
	if !contains(caddyfile, "nginx.dev.local") {
		t.Error("expected nginx.dev.local in Caddyfile")
	}
	if !contains(caddyfile, "nginx.localhost") {
		t.Error("expected nginx.localhost in Caddyfile")
	}
	if !contains(caddyfile, "reverse_proxy localhost:8080") {
		t.Errorf("expected reverse_proxy localhost:8080 in Caddyfile, got: %s", caddyfile)
	}
	if contains(caddyfile, "reverse_proxy nginx:") {
		t.Error("should not use container name in standalone mode")
	}
}

func TestStandaloneSkipsUnpublishedContainers(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "internal", "", "",
			[]container.PortSummary{{PrivatePort: 80, PublicPort: 0}},
			nil, "running"),
	}

	mock := &mockDocker{containers: containers}
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
		StaleTTL:       time.Hour,
		Standalone:     true,
	}

	gen := NewGenerator(cfg, mock)
	ctx := context.Background()

	gen.Refresh(ctx)
	gen.SelectPorts(ctx)

	caddyfile := gen.GenerateCaddyfile()
	if len(caddyfile) > 0 {
		t.Errorf("expected empty Caddyfile for unpublished container in standalone mode, got: %s", caddyfile)
	}
}

func TestStandaloneCustomDomains(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "web", "myapp", "web",
			[]container.PortSummary{{PrivatePort: 3000, PublicPort: 9090}},
			map[string]string{"dev.local.domains": "3000:api.custom.local"},
			"running"),
	}

	mock := &mockDocker{containers: containers}
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
		StaleTTL:       time.Hour,
		Standalone:     true,
	}

	gen := NewGenerator(cfg, mock)
	ctx := context.Background()

	gen.Refresh(ctx)
	gen.SelectPorts(ctx)

	caddyfile := gen.GenerateCaddyfile()
	if !contains(caddyfile, "api.custom.local") {
		t.Error("expected custom domain in Caddyfile")
	}
	if !contains(caddyfile, "reverse_proxy localhost:9090") {
		t.Errorf("expected reverse_proxy localhost:9090 (published port) in Caddyfile, got: %s", caddyfile)
	}
	if contains(caddyfile, "reverse_proxy localhost:3000") {
		t.Error("should use published port, not private port in standalone mode")
	}
	if contains(caddyfile, "myapp.web.localhost") {
		t.Error("localhost domain should not appear when custom domains are set")
	}
}

func TestStandaloneSelectPorts(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "web", "", "",
			[]container.PortSummary{
				{PrivatePort: 3000, PublicPort: 9090},
				{PrivatePort: 8080, PublicPort: 9091},
			},
			nil, "running"),
	}

	mock := &mockDocker{containers: containers}
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
		StaleTTL:       time.Hour,
		Standalone:     true,
		ProbeTimeout:   200 * time.Millisecond,
	}

	gen := NewGenerator(cfg, mock)
	ctx := context.Background()

	gen.Refresh(ctx)
	gen.SelectPorts(ctx)

	infos := gen.Containers()
	if len(infos) != 1 {
		t.Fatalf("expected 1 container, got %d", len(infos))
	}

	// SelectedPort should be 0 since no HTTP server is running on those ports
	if infos[0].SelectedPort != 0 {
		t.Errorf("expected SelectedPort 0 (no HTTP on published ports), got %d", infos[0].SelectedPort)
	}
}

func TestIndexPageStandalone(t *testing.T) {
	containers := []*ContainerInfo{
		{
			ContainerName: "nginx",
			IsCompose:     false,
			IsRunning:     true,
			SelectedPort:  8080,
		},
		{
			Project:       "myapp",
			Service:       "web",
			ContainerName: "web",
			IsCompose:     true,
			IsRunning:     true,
			SelectedPort:  3000,
		},
	}

	page := GenerateIndexPage("dev.local", true, containers)
	if !contains(page, "nginx.dev.local") {
		t.Error("expected nginx.dev.local in index page")
	}
	if !contains(page, "nginx.localhost") {
		t.Error("expected nginx.localhost in index page")
	}
	if !contains(page, "myapp.web.dev.local") {
		t.Error("expected myapp.web.dev.local in index page")
	}
	if !contains(page, "myapp.web.localhost") {
		t.Error("expected myapp.web.localhost in index page")
	}
}

func TestIndexPageNonStandalone(t *testing.T) {
	containers := []*ContainerInfo{
		{
			ContainerName: "nginx",
			IsCompose:     false,
			IsRunning:     true,
			SelectedPort:  80,
		},
	}

	page := GenerateIndexPage("dev.local", false, containers)
	if !contains(page, "nginx.dev.local") {
		t.Error("expected nginx.dev.local in index page")
	}
	if contains(page, "nginx.localhost") {
		t.Error("localhost domain should not appear in non-standalone mode")
	}
}
