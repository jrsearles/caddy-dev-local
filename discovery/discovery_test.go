package discovery

import (
	"context"
	"net/netip"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"go.uber.org/zap"

	"github.com/jrsearles/caddy-dev-local/config"
)

func ip(s string) netip.Addr {
	return netip.MustParseAddr(s)
}

func cloneConfig(c *config.Config) *config.Config {
	cp := *c
	return &cp
}

type mockDocker struct {
	containers []container.Summary
}

func (m *mockDocker) ContainerList(ctx context.Context, options client.ContainerListOptions) ([]container.Summary, error) {
	return m.containers, nil
}

func (m *mockDocker) Events(ctx context.Context, options client.EventsListOptions) (<-chan events.Message, <-chan error) {
	ch := make(chan events.Message)
	errCh := make(chan error)
	return ch, errCh
}

func makeContainer(id, name, project, service string, ports []container.PortSummary, labels map[string]string, state container.ContainerState) container.Summary {
	return makeContainerOnNetworks(id, name, project, service, ports, labels, state, map[string]*network.EndpointSettings{
		"devlocal": {IPAddress: ip("172.18.0.2")},
	})
}

func makeContainerOnNetworks(id, name, project, service string, ports []container.PortSummary, labels map[string]string, state container.ContainerState, networks map[string]*network.EndpointSettings) container.Summary {
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
			Networks: networks,
		},
	}
}

func testDiscovery(t *testing.T, cfg *config.Config, mock *mockDocker) *Discovery {
	t.Helper()
	cfg = cloneConfig(cfg)
	return New(cfg, mock, zap.NewNop())
}

func TestStatusInitial(t *testing.T) {
	d := &Discovery{}
	s := d.Status()
	if s.LastError != "" {
		t.Errorf("expected empty LastError, got %q", s.LastError)
	}
	if !s.LastRefresh.IsZero() {
		t.Error("expected zero LastRefresh")
	}
}

func TestSetError(t *testing.T) {
	d := &Discovery{}
	before := time.Now()
	d.setError("something broke")
	after := time.Now()

	s := d.Status()
	if s.LastError != "something broke" {
		t.Errorf("expected %q, got %q", "something broke", s.LastError)
	}
	if s.LastErrorAt.Before(before) || s.LastErrorAt.After(after) {
		t.Error("LastErrorAt out of expected range")
	}
	if !s.LastRefresh.IsZero() {
		t.Error("LastRefresh should still be zero after setError")
	}
}

func TestRefreshExtractsContainers(t *testing.T) {
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
	cfg := &config.Config{TLD: "dev.local", StaleTTL: time.Hour}

	d := testDiscovery(t, cfg, mock)
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	infos := d.Snapshot()
	if len(infos) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(infos))
	}
	for _, info := range infos {
		if info.ContainerName == "ignored" {
			t.Error("ignored container should not be in results")
		}
	}
}

func TestRefreshDeltaAdded(t *testing.T) {
	mock := &mockDocker{containers: []container.Summary{
		makeContainer("c1", "web", "myapp", "web", []container.PortSummary{{PrivatePort: 80, PublicPort: 0}}, nil, "running"),
	}}
	d := testDiscovery(t, &config.Config{TLD: "dev.local", StaleTTL: time.Hour}, mock)

	delta, err := d.refreshCycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Added) != 1 {
		t.Fatalf("expected 1 added, got %d", len(delta.Added))
	}
	if delta.Added[0].ContainerID != "c1" {
		t.Errorf("added id = %q, want c1", delta.Added[0].ContainerID)
	}
	if len(delta.Updated) != 0 || len(delta.Removed) != 0 {
		t.Errorf("expected no updated/removed, got updated=%d removed=%d", len(delta.Updated), len(delta.Removed))
	}
}

func TestRefreshDeltaUpdatedAndRemoved(t *testing.T) {
	mock := &mockDocker{}
	d := testDiscovery(t, &config.Config{TLD: "dev.local", StaleTTL: time.Hour}, mock)

	mock.containers = []container.Summary{
		makeContainer("c1", "web", "myapp", "web", []container.PortSummary{{PrivatePort: 80, PublicPort: 0}}, nil, "running"),
	}
	if _, err := d.refreshCycle(context.Background()); err != nil {
		t.Fatal(err)
	}

	mock.containers = []container.Summary{
		makeContainer("c1", "web", "myapp", "web", []container.PortSummary{{PrivatePort: 81, PublicPort: 0}}, nil, "running"),
		makeContainer("c2", "api", "myapp", "api", []container.PortSummary{{PrivatePort: 3000, PublicPort: 0}}, nil, "running"),
	}
	delta, err := d.refreshCycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Updated) != 1 || delta.Updated[0].ContainerID != "c1" {
		t.Errorf("expected c1 updated, got %+v", delta.Updated)
	}
	if len(delta.Added) != 1 || delta.Added[0].ContainerID != "c2" {
		t.Errorf("expected c2 added, got %+v", delta.Added)
	}

	mock.containers = []container.Summary{
		makeContainer("c2", "api", "myapp", "api", []container.PortSummary{{PrivatePort: 3000, PublicPort: 0}}, nil, "running"),
	}
	delta, err = d.refreshCycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Removed) != 1 || delta.Removed[0].ContainerID != "c1" {
		t.Errorf("expected c1 removed, got %+v", delta.Removed)
	}
}

func TestRefreshDeltaNoChange(t *testing.T) {
	mock := &mockDocker{}
	d := testDiscovery(t, &config.Config{TLD: "dev.local", StaleTTL: time.Hour}, mock)

	mock.containers = []container.Summary{
		makeContainer("c1", "web", "myapp", "web", []container.PortSummary{{PrivatePort: 80, PublicPort: 0}}, nil, "running"),
	}
	if _, err := d.refreshCycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	delta, err := d.refreshCycle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Added) != 0 || len(delta.Updated) != 0 || len(delta.Removed) != 0 {
		t.Errorf("expected no changes on stable state, got added=%d updated=%d removed=%d", len(delta.Added), len(delta.Updated), len(delta.Removed))
	}
}

func TestCustomDomainsOverride(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "web", "myapp", "web",
			[]container.PortSummary{{PrivatePort: 3000, PublicPort: 0}},
			map[string]string{"dev.local.domains": "3000:api.custom.local"},
			"running"),
	}

	mock := &mockDocker{containers: containers}
	cfg := &config.Config{TLD: "dev.local", StaleTTL: time.Hour}

	d := testDiscovery(t, cfg, mock)
	d.SetEnricher(zeroEnricher())
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	for _, info := range d.Snapshot() {
		if info.ContainerID == "c1" && (info.SelectedPort != 0 || len(info.CustomDomains) != 1) {
			t.Errorf("expected custom domain to suppress port selection and carry domain, got %+v", info)
		}
	}
}

func TestSinglePortSelection(t *testing.T) {
	containers := []container.Summary{
		makeContainer("c1", "nginx", "", "",
			[]container.PortSummary{{PrivatePort: 80, PublicPort: 0}},
			nil, "running"),
	}

	mock := &mockDocker{containers: containers}
	cfg := &config.Config{TLD: "dev.local", StaleTTL: time.Hour}

	d := testDiscovery(t, cfg, mock)
	d.SetEnricher(firstPortEnricher())
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	info := d.Snapshot()[0]
	if info.SelectedPort != 80 {
		t.Errorf("expected selected port 80, got %d", info.SelectedPort)
	}
}

func TestStaleCleanup(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local", StaleTTL: time.Minute}
	d := &Discovery{
		cfg:        cfg,
		containers: make(map[string]*ContainerInfo),
	}

	d.containers["c1"] = &ContainerInfo{
		ContainerID:   "c1",
		ContainerName: "stale",
		IsRunning:     false,
		LastStopped:   time.Now().Add(-2 * time.Minute),
	}
	d.containers["c2"] = &ContainerInfo{
		ContainerID:   "c2",
		ContainerName: "fresh",
		IsRunning:     false,
		LastStopped:   time.Now().Add(-30 * time.Second),
	}

	d.mu.Lock()
	for id, info := range d.containers {
		if !info.IsRunning && !info.LastStopped.IsZero() && time.Since(info.LastStopped) > d.cfg.StaleTTL {
			delete(d.containers, id)
		}
	}
	d.mu.Unlock()

	if _, ok := d.containers["c1"]; ok {
		t.Error("stale container should have been removed")
	}
	if _, ok := d.containers["c2"]; !ok {
		t.Error("fresh stopped container should still exist")
	}
}

func TestStoppedContainerRetainedUntilStaleTTL(t *testing.T) {
	mock := &mockDocker{}
	cfg := &config.Config{TLD: "dev.local", StaleTTL: time.Hour}
	d := testDiscovery(t, cfg, mock)
	d.SetEnricher(firstPortEnricher())
	ctx := context.Background()

	mock.containers = []container.Summary{
		makeContainer("c1", "web", "", "", []container.PortSummary{{PrivatePort: 80, PublicPort: 0}}, nil, "running"),
	}
	if err := d.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	infos := d.Snapshot()
	if len(infos) != 1 || !infos[0].IsRunning {
		t.Fatalf("expected 1 running container, got %+v", infos)
	}
	if infos[0].SelectedPort != 80 {
		t.Fatalf("expected selected port 80, got %d", infos[0].SelectedPort)
	}

	mock.containers = []container.Summary{
		makeContainer("c1", "web", "", "", nil, nil, "exited"),
	}
	if err := d.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	infos = d.Snapshot()
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

	if _, err := d.refreshCycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(d.Snapshot()) != 1 {
		t.Error("stopped container should survive before stale TTL elapses")
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
		{"single port", []container.PortSummary{{PrivatePort: 80, PublicPort: 0}}, []uint16{80}},
		{"multiple ports", []container.PortSummary{{PrivatePort: 3000, PublicPort: 0}, {PrivatePort: 8080, PublicPort: 0}}, []uint16{3000, 8080}},
		{"published ports", []container.PortSummary{{PrivatePort: 80, PublicPort: 8080}}, []uint16{80}},
		{"published and unpublished", []container.PortSummary{{PrivatePort: 80, PublicPort: 8080}, {PrivatePort: 3000, PublicPort: 0}}, []uint16{80, 3000}},
		{"no ports", []container.PortSummary{}, nil},
		{"deduplicates", []container.PortSummary{{PrivatePort: 80, PublicPort: 8080}, {PrivatePort: 80, PublicPort: 0}}, []uint16{80}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &container.Summary{Ports: tt.ports}
			got := extractPorts(c)
			if !slices.Equal(got, tt.want) {
				t.Errorf("extractPorts() = %v, want %v", got, tt.want)
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
		{"single domain", "3000:api.custom.local", []CustomDomain{{Port: 3000, Domain: "api.custom.local"}}},
		{"multiple domains same port", "3000:api.custom.local;3000:api.alt.local", []CustomDomain{{Port: 3000, Domain: "api.custom.local"}, {Port: 3000, Domain: "api.alt.local"}}},
		{"multiple ports", "3000:api.custom.local;8080:admin.custom.local", []CustomDomain{{Port: 3000, Domain: "api.custom.local"}, {Port: 8080, Domain: "admin.custom.local"}}},
		{"empty", "", nil},
		{"invalid no colon", "3000", nil},
		{"invalid no domain", "3000:", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := map[string]string{"dev.local.domains": tt.label}
			got := parseCustomDomains(labels)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseCustomDomains() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHealthAndNetworksPopulated(t *testing.T) {
	cfg := &config.Config{TLD: "dev.local", StaleTTL: time.Hour}

	c := makeContainer("c1", "myapp", "", "", []container.PortSummary{
		{PrivatePort: 8080, PublicPort: 32000, Type: "tcp"},
	}, nil, "running")
	c.Health = &container.HealthSummary{Status: "healthy"}
	c.NetworkSettings = &container.NetworkSettingsSummary{
		Networks: map[string]*network.EndpointSettings{
			"backend":  {IPAddress: ip("172.20.0.2")},
			"frontend": {IPAddress: ip("172.21.0.2")},
			"devlocal": {IPAddress: ip("172.18.0.2")},
		},
	}

	mock := &mockDocker{containers: []container.Summary{c}}
	d := testDiscovery(t, cfg, mock)
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	infos := d.Snapshot()
	if len(infos) != 1 {
		t.Fatalf("expected 1 container, got %d", len(infos))
	}
	info := infos[0]
	if info.Health != "healthy" {
		t.Errorf("expected Health %q, got %q", "healthy", info.Health)
	}
	if !slices.Contains(info.Networks, "backend") || !slices.Contains(info.Networks, "frontend") {
		t.Errorf("expected Networks to contain backend/frontend, got %v", info.Networks)
	}
	if !slices.IsSorted(info.Networks) {
		t.Errorf("expected Networks to be sorted, got %v", info.Networks)
	}
}

func TestHealthEmptyWhenNoHealthcheck(t *testing.T) {
	c := makeContainer("c2", "nohealth", "", "", []container.PortSummary{
		{PrivatePort: 80, PublicPort: 32001, Type: "tcp"},
	}, nil, "running")

	mock := &mockDocker{containers: []container.Summary{c}}
	d := testDiscovery(t, &config.Config{TLD: "dev.local", StaleTTL: time.Hour}, mock)
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if infos := d.Snapshot(); len(infos) != 1 || infos[0].Health != "" {
		t.Errorf("expected empty Health when no HealthSummary, got %+v", infos)
	}
}

func TestSubscriberDispatch(t *testing.T) {
	mock := &mockDocker{}
	d := testDiscovery(t, &config.Config{TLD: "dev.local", StaleTTL: time.Hour}, mock)

	var received []Delta
	d.Subscribe(func(delta Delta) { received = append(received, delta) })
	d.running.Store(true)

	mock.containers = []container.Summary{
		makeContainer("c1", "web", "myapp", "web", []container.PortSummary{{PrivatePort: 80, PublicPort: 0}}, nil, "running"),
	}
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(received) != 1 {
		t.Fatalf("expected 1 dispatched delta, got %d", len(received))
	}
	if len(received[0].Added) != 1 || received[0].Added[0].ContainerID != "c1" {
		t.Errorf("expected c1 added in delta, got %+v", received[0].Added)
	}
	if len(received[0].Snapshot) != 1 {
		t.Errorf("expected snapshot of size 1, got %d", len(received[0].Snapshot))
	}
}

func TestSubscriberInOrder(t *testing.T) {
	mock := &mockDocker{}
	d := testDiscovery(t, &config.Config{TLD: "dev.local", StaleTTL: time.Hour}, mock)
	d.running.Store(true)

	var order []string
	d.Subscribe(func(Delta) { order = append(order, "first") })
	d.Subscribe(func(Delta) { order = append(order, "second") })

	mock.containers = []container.Summary{
		makeContainer("c1", "web", "", "", []container.PortSummary{{PrivatePort: 80, PublicPort: 0}}, nil, "running"),
	}
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(order, []string{"first", "second"}) {
		t.Errorf("subscribers not dispatched in order: %v", order)
	}
}

func TestEnricherInvocation(t *testing.T) {
	mock := &mockDocker{}
	d := testDiscovery(t, &config.Config{TLD: "dev.local", StaleTTL: time.Hour}, mock)

	calls := 0
	d.SetEnricher(func(info *ContainerInfo) {
		calls++
		info.SelectedPort = info.Ports[0]
	})

	mock.containers = []container.Summary{
		makeContainer("c1", "web", "", "", []container.PortSummary{{PrivatePort: 80, PublicPort: 0}}, nil, "running"),
		makeContainer("c2", "stopped", "", "", []container.PortSummary{{PrivatePort: 90, PublicPort: 0}}, nil, "exited"),
	}
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	if calls != 1 {
		t.Errorf("enricher invoked %d times, want 1 (running only)", calls)
	}
	for _, info := range d.Snapshot() {
		if info.IsRunning && info.SelectedPort != 80 {
			t.Errorf("running container SelectedPort = %d, want 80", info.SelectedPort)
		}
		if !info.IsRunning && info.SelectedPort != 0 {
			t.Errorf("stopped container should not be enriched, got SelectedPort %d", info.SelectedPort)
		}
	}
}

func TestRefreshBeforeRunNoDispatch(t *testing.T) {
	mock := &mockDocker{}
	d := testDiscovery(t, &config.Config{TLD: "dev.local", StaleTTL: time.Hour}, mock)

	calls := 0
	d.Subscribe(func(Delta) { calls++ })

	mock.containers = []container.Summary{
		makeContainer("c1", "web", "", "", []container.PortSummary{{PrivatePort: 80, PublicPort: 0}}, nil, "running"),
	}
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("subscriber dispatched before Run, calls=%d", calls)
	}
}

func firstPortEnricher() Enricher {
	return func(info *ContainerInfo) {
		if len(info.Ports) > 0 {
			info.SelectedPort = info.Ports[0]
		}
	}
}

func zeroEnricher() Enricher {
	return func(info *ContainerInfo) {
		info.SelectedPort = 0
	}
}
