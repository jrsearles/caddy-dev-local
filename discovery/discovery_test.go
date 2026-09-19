package discovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

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
	listErr    error
	listFn     func(context.Context) ([]container.Summary, error)
	eventsFn   func()
	eventCalls atomic.Int32
}

func (m *mockDocker) ContainerList(ctx context.Context, options client.ContainerListOptions) ([]container.Summary, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return m.containers, m.listErr
}

func (m *mockDocker) Events(ctx context.Context, options client.EventsListOptions) (<-chan events.Message, <-chan error) {
	m.eventCalls.Add(1)
	if m.eventsFn != nil {
		m.eventsFn()
	}
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
	return New(cfg, mock, slog.New(slog.DiscardHandler))
}

func testDiscoveryWithProbe(t *testing.T, cfg *config.Config, mock *mockDocker, probe PortProbe) *Discovery {
	t.Helper()
	cfg = cloneConfig(cfg)
	return New(cfg, mock, slog.New(slog.DiscardHandler), WithPortProbe(probe))
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
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	for _, info := range d.Snapshot() {
		if info.ContainerID == "c1" && (info.SelectedPort != 0 || len(info.CustomDomains) != 1) {
			t.Errorf("expected custom domain present and SelectedPort zero, got %+v", info)
		}
	}
}

func TestRefreshProbesOnlyPublishedPorts(t *testing.T) {
	mock := &mockDocker{containers: []container.Summary{
		makeContainer("c1", "web", "", "", []container.PortSummary{
			{PrivatePort: 80, PublicPort: 32080},
			{PrivatePort: 3000},
			{PrivatePort: 8080, PublicPort: 32080},
		}, nil, "running"),
	}}
	var probed []uint16
	probe := func(_ context.Context, host string, ports []uint16, timeout time.Duration) (uint16, error) {
		if host != "localhost" {
			t.Errorf("host = %q, want localhost", host)
		}
		probed = slices.Clone(ports)
		return ports[0], nil
	}
	d := testDiscoveryWithProbe(t, &config.Config{StaleTTL: time.Hour, ProbeTimeout: time.Second}, mock, probe)
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(probed, []uint16{32080}) {
		t.Fatalf("probed ports = %v, want only published host port 32080", probed)
	}
	if got := d.Snapshot()[0].SelectedPort; got != 32080 {
		t.Fatalf("SelectedPort = %d, want 32080", got)
	}
}

func TestRefreshSkipsProbeWithoutPublishedPorts(t *testing.T) {
	mock := &mockDocker{containers: []container.Summary{
		makeContainer("c1", "web", "", "", []container.PortSummary{{PrivatePort: 80}}, nil, "running"),
	}}
	calls := 0
	d := testDiscoveryWithProbe(t, &config.Config{StaleTTL: time.Hour}, mock, func(context.Context, string, []uint16, time.Duration) (uint16, error) {
		calls++
		return 0, nil
	})
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("probe called %d times, want 0", calls)
	}
}

func TestRefreshRetainsSelectedPortForStoppedContainer(t *testing.T) {
	mock := &mockDocker{containers: []container.Summary{
		makeContainer("c1", "web", "", "", []container.PortSummary{{PrivatePort: 80, PublicPort: 32080}}, nil, "running"),
	}}
	probeErr := false
	d := testDiscoveryWithProbe(t, &config.Config{StaleTTL: time.Hour}, mock, func(_ context.Context, _ string, ports []uint16, _ time.Duration) (uint16, error) {
		if probeErr {
			return 0, errors.New("not HTTP")
		}
		return ports[0], nil
	})
	ctx := context.Background()
	if err := d.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	probeErr = true
	if err := d.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if got := d.Snapshot()[0].SelectedPort; got != 0 {
		t.Fatalf("SelectedPort after failed running probe = %d, want 0", got)
	}

	mock.containers = []container.Summary{
		makeContainer("c1", "web", "", "", nil, nil, "exited"),
	}
	if err := d.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if got := d.Snapshot()[0].SelectedPort; got != 32080 {
		t.Fatalf("stopped SelectedPort = %d, want cached 32080", got)
	}
}

func TestRefreshEvictsSelectedPortWhenContainerDisappears(t *testing.T) {
	mock := &mockDocker{containers: []container.Summary{
		makeContainer("c1", "web", "", "", []container.PortSummary{{PrivatePort: 80, PublicPort: 32080}}, nil, "running"),
	}}
	d := testDiscoveryWithProbe(t, &config.Config{StaleTTL: time.Hour}, mock, func(_ context.Context, _ string, ports []uint16, _ time.Duration) (uint16, error) {
		return ports[0], nil
	})
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	mock.containers = nil
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.selectedPorts["c1"]; ok {
		t.Fatal("selected port cache retained a disappeared container")
	}
}

func TestRefreshCommitsEnrichedSnapshotAtomically(t *testing.T) {
	mock := &mockDocker{containers: []container.Summary{
		makeContainer("c1", "web", "", "", []container.PortSummary{{PrivatePort: 80, PublicPort: 32080}}, nil, "running"),
	}}
	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	calls := 0
	d := testDiscoveryWithProbe(t, &config.Config{StaleTTL: time.Hour}, mock, func(_ context.Context, _ string, ports []uint16, _ time.Duration) (uint16, error) {
		calls++
		if calls == 2 {
			close(probeStarted)
			<-releaseProbe
		}
		return ports[0], nil
	})
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	mock.containers = []container.Summary{
		makeContainer("c1", "web", "", "", []container.PortSummary{{PrivatePort: 80, PublicPort: 32081}}, nil, "running"),
	}
	refreshDone := make(chan error, 1)
	go func() {
		refreshDone <- d.Refresh(context.Background())
	}()
	<-probeStarted

	during := d.Snapshot()[0]
	if during.SelectedPort != 32080 || during.PublishedPorts[80] != 32080 {
		t.Fatalf("snapshot changed before enrichment completed: %+v", during)
	}
	close(releaseProbe)
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	after := d.Snapshot()[0]
	if after.SelectedPort != 32081 || after.PublishedPorts[80] != 32081 {
		t.Fatalf("enriched snapshot was not committed: %+v", after)
	}
}

func TestRefreshSkipsCustomDomainProbe(t *testing.T) {
	mock := &mockDocker{containers: []container.Summary{
		makeContainer("c1", "web", "", "", []container.PortSummary{{PrivatePort: 80, PublicPort: 32080}}, map[string]string{
			"dev.local.domains": "80:web.example.test",
		}, "running"),
	}}
	calls := 0
	d := testDiscoveryWithProbe(t, &config.Config{StaleTTL: time.Hour}, mock, func(context.Context, string, []uint16, time.Duration) (uint16, error) {
		calls++
		return 0, nil
	})
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("probe called %d times, want 0", calls)
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
	if info.SelectedPort != 0 {
		t.Errorf("expected SelectedPort to be 0 without enrichment, got %d", info.SelectedPort)
	}

	if err := d.refreshCycle(context.Background()); err != nil {
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

func TestRunSubscribesToEventsBeforeInitialRefresh(t *testing.T) {
	var listed atomic.Bool
	var eventBeforeList atomic.Bool
	mock := &mockDocker{}
	mock.listFn = func(context.Context) ([]container.Summary, error) {
		listed.Store(true)
		return []container.Summary{
			makeContainer("c1", "web", "", "", []container.PortSummary{{PrivatePort: 80}}, nil, "running"),
		}, nil
	}
	mock.eventsFn = func() {
		if !listed.Load() {
			eventBeforeList.Store(true)
		}
	}
	d := testDiscovery(t, &config.Config{TLD: "dev.local", StaleTTL: time.Hour}, mock)
	ctx, cancel := context.WithCancel(context.Background())
	d.Run(ctx)

	update := receiveUpdate(t, d.Updates())
	if len(update.Snapshot) != 1 || update.Snapshot[0].ContainerID != "c1" || update.Status.LastRefresh.IsZero() {
		t.Fatalf("unexpected initial update: %+v", update)
	}
	waitFor(t, func() bool { return mock.eventCalls.Load() == 1 })
	if !eventBeforeList.Load() {
		t.Fatal("event subscription started after initial container list")
	}
	cancel()
	waitForClosed(t, d.Updates())
}

func TestProbeHTTPPort(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	_, portStr, _ := net.SplitHostPort(ts.Listener.Addr().String())
	var port uint16
	fmt.Sscanf(portStr, "%d", &port) //nolint:errcheck // test helper, value validated below

	got, err := ProbeHTTPPort(context.Background(), "localhost", []uint16{port}, 2*time.Second)
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

	_, err = ProbeHTTPPort(context.Background(), "localhost", []uint16{port}, 500*time.Millisecond)
	if err == nil {
		t.Fatal("ProbeHTTPPort() should have returned error for non-HTTP server")
	}
}

func TestProbeHTTPPortCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ProbeHTTPPort(ctx, "localhost", []uint16{1}, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ProbeHTTPPort() error = %v, want context canceled", err)
	}
}

func TestUpdatesLatestWins(t *testing.T) {
	d := testDiscovery(t, &config.Config{}, &mockDocker{})
	for _, id := range []string{"c1", "c2", "c3"} {
		d.publish(Update{Snapshot: []*ContainerInfo{{ContainerID: id}}})
	}

	update := receiveUpdate(t, d.Updates())
	if got := update.Snapshot[0].ContainerID; got != "c3" {
		t.Fatalf("latest update id = %q, want c3", got)
	}
}

func TestSnapshotsAndUpdatesAreDeepCloned(t *testing.T) {
	labels := map[string]string{"dev.local.domains": "80:web.example.test", "source": "docker"}
	mock := &mockDocker{containers: []container.Summary{
		makeContainer("c1", "web", "", "", []container.PortSummary{{PrivatePort: 80, PublicPort: 8080}}, labels, "running"),
	}}
	d := testDiscovery(t, &config.Config{TLD: "dev.local", StaleTTL: time.Hour}, mock)
	ctx, cancel := context.WithCancel(context.Background())
	d.Run(ctx)
	received := receiveUpdate(t, d.Updates())

	assertUnmutated := func(t *testing.T, info *ContainerInfo) {
		t.Helper()
		if info.ContainerName != "web" || info.Ports[0] != 80 || info.PublishedPorts[80] != 8080 ||
			info.Labels["source"] != "docker" || info.CustomDomains[0].Domain != "web.example.test" || info.Networks[0] != "devlocal" {
			t.Fatalf("container was mutated through a consumer copy: %+v", info)
		}
	}
	assertUnmutated(t, received.Snapshot[0])
	received.Snapshot[0].ContainerName = "mutated"
	received.Snapshot[0].Ports[0] = 999
	received.Snapshot[0].PublishedPorts[80] = 999
	received.Snapshot[0].Labels["source"] = "mutated"
	received.Snapshot[0].CustomDomains[0].Domain = "mutated.test"
	received.Snapshot[0].Networks[0] = "mutated"

	snapshot := d.Snapshot()
	assertUnmutated(t, snapshot[0])
	snapshot[0].Ports[0] = 123
	snapshot[0].Labels["source"] = "snapshot mutation"
	assertUnmutated(t, d.Snapshot()[0])
	cancel()
	waitForClosed(t, d.Updates())
}

func TestListErrorPublishesStatusAndRetainsSnapshot(t *testing.T) {
	mock := &mockDocker{containers: []container.Summary{
		makeContainer("c1", "web", "", "", []container.PortSummary{{PrivatePort: 80}}, nil, "running"),
	}}
	d := testDiscovery(t, &config.Config{TLD: "dev.local", StaleTTL: time.Hour}, mock)
	ctx, cancel := context.WithCancel(context.Background())
	d.Run(ctx)
	receiveUpdate(t, d.Updates())
	mock.listErr = errors.New("docker unavailable")
	if err := d.Refresh(context.Background()); err == nil {
		t.Fatal("expected list error")
	}

	update := receiveUpdate(t, d.Updates())
	if update.Status.LastError != "docker unavailable" || update.Status.LastErrorAt.IsZero() {
		t.Errorf("unexpected error status: %+v", update.Status)
	}
	if len(update.Snapshot) != 1 || update.Snapshot[0].ContainerID != "c1" {
		t.Errorf("error update did not retain snapshot: %+v", update.Snapshot)
	}
	if len(d.Snapshot()) != 1 {
		t.Fatal("list error cleared retained state")
	}
	cancel()
	waitForClosed(t, d.Updates())
}

func TestConcurrentRefreshesCommitAndPublishInOrder(t *testing.T) {
	firstEntered := make(chan struct{})
	secondEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	releaseSecond := make(chan struct{})
	var listCalls atomic.Int32
	mock := &mockDocker{}
	mock.listFn = func(context.Context) ([]container.Summary, error) {
		switch listCalls.Add(1) {
		case 1:
			close(firstEntered)
			<-releaseFirst
			return []container.Summary{makeContainer("c1", "first", "", "", nil, nil, "running")}, nil
		case 2:
			close(secondEntered)
			<-releaseSecond
			return []container.Summary{makeContainer("c2", "second", "", "", nil, nil, "running")}, nil
		default:
			return nil, errors.New("unexpected list call")
		}
	}
	d := testDiscovery(t, &config.Config{TLD: "dev.local", StaleTTL: time.Hour}, mock)
	d.running.Store(true)

	errs := make(chan error, 2)
	go func() { errs <- d.Refresh(context.Background()) }()
	<-firstEntered
	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		errs <- d.Refresh(context.Background())
	}()
	<-secondStarted

	select {
	case <-secondEntered:
		t.Fatal("second refresh listed containers before first refresh completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseFirst)
	<-secondEntered
	first := receiveUpdate(t, d.Updates())
	if got := first.Snapshot[0].ContainerID; got != "c1" {
		t.Fatalf("first update id = %q, want c1", got)
	}
	close(releaseSecond)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	second := receiveUpdate(t, d.Updates())
	if got := second.Snapshot[0].ContainerID; got != "c2" {
		t.Fatalf("second update id = %q, want c2", got)
	}
}

func TestRunIsOneShotAndClosesUpdates(t *testing.T) {
	mock := &mockDocker{}
	d := testDiscovery(t, &config.Config{TLD: "dev.local", StaleTTL: time.Hour}, mock)
	ctx, cancel := context.WithCancel(context.Background())
	d.Run(ctx)
	d.Run(ctx)

	receiveUpdate(t, d.Updates())
	waitFor(t, func() bool { return mock.eventCalls.Load() == 1 })
	if got := mock.eventCalls.Load(); got != 1 {
		t.Fatalf("Events called %d times after duplicate Run", got)
	}

	cancel()
	waitForClosed(t, d.Updates())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	d.Run(ctx2)
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if got := mock.eventCalls.Load(); got != 1 {
		t.Fatalf("Events called %d times after second Run, want 1", got)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for condition")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRefreshBeforeRunDoesNotPublish(t *testing.T) {
	mock := &mockDocker{}
	d := testDiscovery(t, &config.Config{TLD: "dev.local", StaleTTL: time.Hour}, mock)

	mock.containers = []container.Summary{
		makeContainer("c1", "web", "", "", []container.PortSummary{{PrivatePort: 80, PublicPort: 0}}, nil, "running"),
	}
	if err := d.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	select {
	case <-d.Updates():
		t.Fatal("unexpected update before Run")
	case <-ctx.Done():
	}
}

func receiveUpdate(t *testing.T, updates <-chan Update) Update {
	t.Helper()
	select {
	case update, ok := <-updates:
		if !ok {
			t.Fatal("updates channel closed while waiting for update")
		}
		return update
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for update")
	}
	return Update{}
}

func waitForClosed(t *testing.T, updates <-chan Update) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-updates:
			if !ok {
				return
			}
		case <-timer.C:
			t.Fatal("timed out waiting for updates channel to close")
		}
	}
}
