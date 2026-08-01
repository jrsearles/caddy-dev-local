package generator

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/jsearles/caddy-dev-local/config"
	"github.com/jsearles/caddy-dev-local/docker"
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
		ID:     id,
		Names:  []string{"/" + name},
		Ports:  ports,
		Labels: labels,
		State:  state,
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
			c := &container.Summary{Labels: tt.labels}
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
			c := &container.Summary{Ports: tt.ports}
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

func TestParseCustomDomains(t *testing.T) {
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
			labels := map[string]string{"dev.local.domains": tt.label}
			got := parseCustomDomains(labels)
			if len(got) != len(tt.want) {
				t.Errorf("parseCustomDomains() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseCustomDomains()[%d] = %v, want %v", i, got[i], tt.want[i])
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

	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	targets := gen.DomainTargets()
	if len(targets) != 1 {
		t.Fatalf("expected 1 domain, got %d: %v", len(targets), targets)
	}
	if _, ok := targets["api.custom.local"]; !ok {
		t.Error("expected custom domain in targets")
	}
	if _, ok := targets["myapp.web.dev.local"]; ok {
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
	gen.probeFn = func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		return ports[0], nil
	}
	ctx := context.Background()

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	targets := gen.DomainTargets()
	if !slices.Equal(targets["nginx.dev.local"], []string{"nginx:80"}) {
		t.Errorf("expected reverse proxy target nginx:80, got %v", targets["nginx.dev.local"])
	}
	if _, ok := targets["nginx.localhost"]; ok {
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

func TestStoppedContainerRetainedUntilStaleTTL(t *testing.T) {
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
		StaleTTL:       time.Hour,
	}

	mock := &mockDocker{}
	gen := NewGenerator(cfg, mock)
	gen.probeFn = func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		return ports[0], nil
	}
	ctx := context.Background()

	mock.containers = []container.Summary{
		makeContainer("c1", "web", "", "",
			[]container.PortSummary{{PrivatePort: 80, PublicPort: 0}},
			nil, "running"),
	}
	gen.RefreshAndSelect(ctx) //nolint:errcheck // test helper

	infos := gen.Containers()
	if len(infos) != 1 || !infos[0].IsRunning {
		t.Fatalf("expected 1 running container, got %+v", infos)
	}
	if infos[0].SelectedPort != 80 {
		t.Fatalf("expected selected port 80, got %d", infos[0].SelectedPort)
	}

	mock.containers = []container.Summary{
		makeContainer("c1", "web", "", "",
			nil, nil, "exited"),
	}
	gen.Refresh(ctx) //nolint:errcheck // test helper

	infos = gen.Containers()
	if len(infos) != 1 {
		t.Fatalf("expected stopped container to be retained, got %d", len(infos))
	}
	info := infos[0]
	if info.IsRunning {
		t.Error("container should not be running")
	}
	if info.LastStopped.IsZero() {
		t.Error("LastStopped should be set when a container stops")
	}
	if len(info.Ports) != 1 || info.Ports[0] != 80 {
		t.Errorf("expected ports to be preserved, got %v", info.Ports)
	}
	if info.SelectedPort != 80 {
		t.Errorf("expected SelectedPort to be preserved, got %d", info.SelectedPort)
	}

	page := GenerateIndexPage("dev.local", false, gen.Containers())
	if !contains(page, "web.dev.local") {
		t.Error("expected stopped container on index page")
	}

	gen.StaleCleanup()
	if len(gen.Containers()) != 1 {
		t.Error("stopped container should survive before stale TTL elapses")
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

	if !docker.HasNetwork(&c, "devlocal") {
		t.Error("expected HasNetwork to return true for devlocal")
	}
	if docker.HasNetwork(&c, "missing") {
		t.Error("expected HasNetwork to return false for missing")
	}
}

func TestContainerName(t *testing.T) {
	c := container.Summary{
		Names: []string{"/my-container"},
	}
	got := docker.ContainerName(&c)
	if got != "my-container" {
		t.Errorf("ContainerName() = %q, want %q", got, "my-container")
	}

	c2 := container.Summary{
		Names: []string{},
		ID:    "abc123def456",
	}
	got2 := docker.ContainerName(&c2)
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
	gen.probeFn = func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		return ports[0], nil
	}
	ctx := context.Background()

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	targets := gen.DomainTargets()
	if !slices.Equal(targets["nginx.dev.local"], []string{"localhost:8080"}) {
		t.Errorf("expected reverse_proxy localhost:8080 in standalone mode, got %v", targets["nginx.dev.local"])
	}
	if !slices.Equal(targets["nginx.localhost"], []string{"localhost:8080"}) {
		t.Errorf("expected nginx.localhost target, got %v", targets["nginx.localhost"])
	}
	for _, upstreams := range targets {
		for _, u := range upstreams {
			if strings.HasPrefix(u, "nginx:") {
				t.Error("should not use container name in standalone mode")
			}
		}
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

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	if targets := gen.DomainTargets(); len(targets) > 0 {
		t.Errorf("expected no targets for unpublished container in standalone mode, got %v", targets)
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

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	targets := gen.DomainTargets()
	if !slices.Equal(targets["api.custom.local"], []string{"localhost:9090"}) {
		t.Errorf("expected custom domain target localhost:9090 (published port), got %v", targets["api.custom.local"])
	}
	for _, upstreams := range targets {
		for _, u := range upstreams {
			if u == "localhost:3000" {
				t.Error("should use published port, not private port in standalone mode")
			}
		}
	}
	if _, ok := targets["myapp.web.localhost"]; ok {
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
	gen.probeFn = func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		return 0, fmt.Errorf("no HTTP port found")
	}

	ctx := context.Background()
	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	infos := gen.Containers()
	if len(infos) != 1 {
		t.Fatalf("expected 1 container, got %d", len(infos))
	}

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

func TestIndexPageIPAddressDockerMode(t *testing.T) {
	containers := []*ContainerInfo{
		{
			ContainerName: "nginx",
			IsCompose:     false,
			IsRunning:     true,
			SelectedPort:  80,
			IPAddress:     "172.18.0.2",
		},
	}

	page := GenerateIndexPage("dev.local", false, containers)
	if !contains(page, "172.18.0.2") {
		t.Error("expected container IP in index page for Docker mode")
	}
}

func TestIndexPageIPAddressStandalone(t *testing.T) {
	containers := []*ContainerInfo{
		{
			ContainerName: "nginx",
			IsCompose:     false,
			IsRunning:     true,
			SelectedPort:  8080,
			IPAddress:     "172.18.0.2",
		},
	}

	page := GenerateIndexPage("dev.local", true, containers)
	if contains(page, "172.18.0.2") {
		t.Error("expected container IP to be absent in standalone mode")
	}
}

func TestIndexPageIPAddressCustomDomainDockerMode(t *testing.T) {
	containers := []*ContainerInfo{
		{
			ContainerName: "web",
			IsRunning:     true,
			IPAddress:     "172.18.0.3",
			CustomDomains: []CustomDomain{
				{Port: 3000, Domain: "api.custom.local"},
			},
		},
	}

	page := GenerateIndexPage("dev.local", false, containers)
	if !contains(page, "172.18.0.3") {
		t.Error("expected container IP in index page for custom domains in Docker mode")
	}
	if !contains(page, "api.custom.local") {
		t.Error("expected custom domain in index page")
	}
}

func TestIndexPageStoppedContainer(t *testing.T) {
	stopped := time.Date(2026, 7, 28, 12, 30, 0, 0, time.UTC)
	containers := []*ContainerInfo{
		{
			ContainerName: "api",
			IsCompose:     false,
			IsRunning:     false,
			LastStopped:   stopped,
			SelectedPort:  9090,
		},
	}

	page := GenerateIndexPage("dev.local", false, containers)
	if !contains(page, "api.dev.local") {
		t.Error("expected api.dev.local in index page")
	}
	if !contains(page, "status-stopped") {
		t.Error("expected stopped status indicator")
	}
	if !contains(page, "stopped") {
		t.Error("expected 'stopped' text")
	}
	if !contains(page, "2026-07-28 12:30") {
		t.Error("expected stopped timestamp")
	}
	if !contains(page, "class=\"stopped\"") {
		t.Error("expected stopped row class")
	}
}

func TestIndexPageCustomDomains(t *testing.T) {
	containers := []*ContainerInfo{
		{
			ContainerName: "web",
			IsRunning:     true,
			CustomDomains: []CustomDomain{
				{Port: 3000, Domain: "api.custom.local"},
				{Port: 8080, Domain: "admin.custom.local"},
			},
		},
		{
			ContainerName: "nginx",
			IsRunning:     true,
			SelectedPort:  80,
		},
	}

	page := GenerateIndexPage("dev.local", false, containers)
	if !contains(page, "api.custom.local") {
		t.Error("expected api.custom.local in index page")
	}
	if !contains(page, "admin.custom.local") {
		t.Error("expected admin.custom.local in index page")
	}
	if !contains(page, "nginx.dev.local") {
		t.Error("expected nginx.dev.local in index page")
	}
	if contains(page, "web.dev.local") {
		t.Error("auto-generated domain should not appear when custom domains are set")
	}
}

func TestIndexPagePortsWithoutHTTP(t *testing.T) {
	containers := []*ContainerInfo{
		{
			ContainerName:  "web",
			Project:        "myapp",
			Service:        "web",
			IsCompose:      true,
			IsRunning:      true,
			Ports:          []uint16{3000},
			PublishedPorts: map[uint16]uint16{3000: 9090},
		},
		{
			ContainerName:  "nginx",
			IsRunning:      true,
			Ports:          []uint16{80},
			PublishedPorts: map[uint16]uint16{80: 8080},
		},
		{
			ContainerName:  "api",
			IsRunning:      true,
			Ports:          []uint16{4000, 5000},
			PublishedPorts: map[uint16]uint16{4000: 9000, 5000: 9001},
		},
	}

	page := GenerateIndexPage("dev.local", false, containers)
	if !contains(page, "myapp.web.dev.local") {
		t.Error("expected myapp.web.dev.local in index page")
	}
	if !contains(page, "nginx.dev.local") {
		t.Error("expected nginx.dev.local in index page")
	}
	if !contains(page, ":3000") {
		t.Error("expected port 3000 for compose container in docker mode")
	}
	if !contains(page, ":80") {
		t.Error("expected port 80 for standalone container in docker mode")
	}
	if contains(page, "href=\"http://") {
		t.Error("expected no http links for containers without HTTP in docker mode")
	}
	if contains(page, "href=\"https://") {
		t.Error("expected no https links for containers without HTTP in docker mode")
	}

	pageStandalone := GenerateIndexPage("dev.local", true, containers)
	if !contains(pageStandalone, "nginx.localhost") {
		t.Error("expected nginx.localhost in standalone index page")
	}
	if !contains(pageStandalone, ":9090") {
		t.Error("expected published port 9090 in standalone mode")
	}
	if !contains(pageStandalone, ":8080") {
		t.Error("expected published port 8080 in standalone mode")
	}
	if contains(pageStandalone, "href=\"") {
		t.Error("expected no links at all for containers without HTTP in standalone mode")
	}
	if !contains(page, ":4000, 5000") {
		t.Error("expected multiple ports 4000, 5000 for multi-port container in docker mode")
	}
	if !contains(pageStandalone, ":9000, 9001") {
		t.Error("expected published ports 9000, 9001 for multi-port container in standalone mode")
	}
}

func TestDomainsComposeAndStandalone(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "web", "myapp", "web",
			[]container.PortSummary{{PrivatePort: 3000, PublicPort: 0}},
			nil, "running"),
		makeContainer("c2", "nginx", "", "",
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
	gen.probeFn = func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		return ports[0], nil
	}
	ctx := context.Background()

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	domains := gen.Domains()
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d: %v", len(domains), domains)
	}
	if domains[0] != "myapp.web.dev.local" {
		t.Errorf("expected myapp.web.dev.local, got %s", domains[0])
	}
	if domains[1] != "nginx.dev.local" {
		t.Errorf("expected nginx.dev.local, got %s", domains[1])
	}
}

func TestDomainsIncludesLocalhost(t *testing.T) {
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
	gen.probeFn = func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		return ports[0], nil
	}
	ctx := context.Background()

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	domains := gen.Domains()
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d: %v", len(domains), domains)
	}
	if domains[0] != "nginx.dev.local" {
		t.Errorf("expected nginx.dev.local, got %s", domains[0])
	}
	if domains[1] != "nginx.localhost" {
		t.Errorf("expected nginx.localhost, got %s", domains[1])
	}
}

func TestDomainsCustomDomains(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "web", "myapp", "web",
			[]container.PortSummary{{PrivatePort: 3000, PublicPort: 0}},
			map[string]string{"dev.local.domains": "3000:api.custom.local;3000:admin.custom.local"},
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

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	domains := gen.Domains()
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d: %v", len(domains), domains)
	}
	if domains[0] != "admin.custom.local" {
		t.Errorf("expected admin.custom.local, got %s", domains[0])
	}
	if domains[1] != "api.custom.local" {
		t.Errorf("expected api.custom.local, got %s", domains[1])
	}
}

func TestDomainsExcludesStopped(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "web", "myapp", "web",
			[]container.PortSummary{{PrivatePort: 3000, PublicPort: 0}},
			nil, "running"),
		makeContainer("c2", "api", "myapp", "api",
			[]container.PortSummary{{PrivatePort: 3000, PublicPort: 0}},
			nil, "exited"),
	}

	mock := &mockDocker{containers: containers}
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
		StaleTTL:       time.Hour,
	}

	gen := NewGenerator(cfg, mock)
	gen.probeFn = func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		return ports[0], nil
	}
	ctx := context.Background()

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	domains := gen.Domains()
	if len(domains) != 1 {
		t.Fatalf("expected 1 domain, got %d: %v", len(domains), domains)
	}
	if domains[0] != "myapp.web.dev.local" {
		t.Errorf("expected myapp.web.dev.local, got %s", domains[0])
	}
}

func TestDomainsSorted(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "zebra", "", "",
			[]container.PortSummary{{PrivatePort: 80, PublicPort: 0}},
			nil, "running"),
		makeContainer("c2", "alpha", "", "",
			[]container.PortSummary{{PrivatePort: 80, PublicPort: 0}},
			nil, "running"),
		makeContainer("c3", "middle", "", "",
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
	gen.probeFn = func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		return ports[0], nil
	}
	ctx := context.Background()

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	domains := gen.Domains()
	expected := []string{"alpha.dev.local", "middle.dev.local", "zebra.dev.local"}
	if len(domains) != len(expected) {
		t.Fatalf("expected %d domains, got %d: %v", len(expected), len(domains), domains)
	}
	for i, d := range domains {
		if d != expected[i] {
			t.Errorf("domains[%d] = %s, want %s", i, d, expected[i])
		}
	}
}

func TestDomainsIncludesPortsWithoutHTTP_DockerMode(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "web", "myapp", "web",
			[]container.PortSummary{{PrivatePort: 3000, PublicPort: 0}},
			nil, "running"),
	}

	mock := &mockDocker{containers: containers}
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
		StaleTTL:       time.Hour,
		Standalone:     false,
	}

	gen := NewGenerator(cfg, mock)
	gen.probeFn = func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		return 0, fmt.Errorf("no HTTP port found")
	}
	ctx := context.Background()

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	domains := gen.Domains()
	if len(domains) != 1 {
		t.Fatalf("expected 1 domain, got %d: %v", len(domains), domains)
	}
	if domains[0] != "myapp.web.dev.local" {
		t.Errorf("expected myapp.web.dev.local, got %s", domains[0])
	}
}

func TestDomainsIncludesPortsWithoutHTTP_Standalone(t *testing.T) {
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
	gen.probeFn = func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		return 0, fmt.Errorf("no HTTP port found")
	}
	ctx := context.Background()

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	domains := gen.Domains()
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d: %v", len(domains), domains)
	}
	if domains[0] != "nginx.dev.local" {
		t.Errorf("expected nginx.dev.local, got %s", domains[0])
	}
	if domains[1] != "nginx.localhost" {
		t.Errorf("expected nginx.localhost, got %s", domains[1])
	}
}

func TestDomainsExcludesUnpublishedInStandalone(t *testing.T) {
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

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	domains := gen.Domains()
	if len(domains) != 0 {
		t.Errorf("expected 0 domains for unpublished container in standalone mode, got %d: %v", len(domains), domains)
	}
}

func TestDomainsExcludesPortless(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "web", "myapp", "web",
			nil,
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

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	domains := gen.Domains()
	if len(domains) != 0 {
		t.Errorf("expected 0 domains for portless container in Docker mode, got %d: %v", len(domains), domains)
	}
}

func TestDomainTargetsMergesDuplicateDomains(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "web", "myapp", "web",
			[]container.PortSummary{{PrivatePort: 3000, PublicPort: 0}},
			nil, "running"),
		makeContainer("c2", "web", "myapp", "web",
			[]container.PortSummary{{PrivatePort: 3001, PublicPort: 0}},
			nil, "running"),
	}

	mock := &mockDocker{containers: containers}
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
		StaleTTL:       time.Hour,
	}

	gen := NewGenerator(cfg, mock)
	gen.probeFn = func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		return ports[0], nil
	}
	ctx := context.Background()

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	targets := gen.DomainTargets()
	if len(targets) != 1 {
		t.Fatalf("expected 1 merged domain, got %d: %v", len(targets), targets)
	}
	got := targets["myapp.web.dev.local"]
	want := []string{"web:3000", "web:3001"}
	if !slices.Equal(got, want) {
		t.Errorf("DomainTargets() = %v, want %v", got, want)
	}
}

func TestDomainTargetsStandalone(t *testing.T) {
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
	gen.probeFn = func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		return ports[0], nil
	}
	ctx := context.Background()

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	targets := gen.DomainTargets()
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
	containers := []container.Summary{
		makeContainer("c1", "web", "myapp", "web",
			[]container.PortSummary{{PrivatePort: 3000, PublicPort: 0}},
			map[string]string{"dev.local.domains": "3000:api.custom.local;8080:admin.custom.local"},
			"running"),
	}

	mock := &mockDocker{containers: containers}
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
		StaleTTL:       time.Hour,
	}

	gen := NewGenerator(cfg, mock)
	gen.probeFn = func(host string, ports []uint16, timeout time.Duration) (uint16, error) {
		return ports[0], nil
	}
	ctx := context.Background()

	gen.Refresh(ctx)     //nolint:errcheck // test helper
	gen.SelectPorts(ctx) //nolint:errcheck // test helper

	targets := gen.DomainTargets()
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
