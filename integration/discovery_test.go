//go:build integration

package integration

import (
	"testing"
	"time"
)

func TestEventFlowAdded(t *testing.T) {
	d := newDiscovery(t)
	collector := collectDeltas()
	d.Subscribe(collector.handler)

	name := "it-added"
	startTarget(t, targetOpts{name: name})

	waitForContainers(t, d, name)
	waitForAdded(t, collector, name)

	if infoByName(d, name) == nil {
		t.Fatalf("container %s missing from snapshot", name)
	}
}

func TestEventFlowUpdated(t *testing.T) {
	d := newDiscovery(t)
	collector := collectDeltas()
	d.Subscribe(collector.handler)

	name := "it-updated"
	tg := startTarget(t, targetOpts{name: name})

	waitForContainers(t, d, name)
	waitForAdded(t, collector, name)

	tg.stop(t)
	waitForDeltaField(t, collector, name, "updated")
}

func TestEventFlowRemovedRunning(t *testing.T) {
	cfg := *sharedConfig
	cfg.PollInterval = 500 * time.Millisecond
	d := newDiscoveryWithConfig(t, &cfg)
	collector := collectDeltas()
	d.Subscribe(collector.handler)

	name := "it-removed"
	tg := startTarget(t, targetOpts{name: name})

	waitForContainers(t, d, name)
	waitForAdded(t, collector, name)

	tg.forceRemove(t)

	waitForRemoved(t, collector, name)
	waitForAbsent(t, d, name)
}

func TestEventFlowRemovedStale(t *testing.T) {
	cfg := *sharedConfig
	cfg.PollInterval = 500 * time.Millisecond
	d := newDiscoveryWithConfig(t, &cfg)
	collector := collectDeltas()
	d.Subscribe(collector.handler)

	name := "it-stale"
	tg := startTarget(t, targetOpts{name: name})

	waitForContainers(t, d, name)
	waitForAdded(t, collector, name)

	tg.stop(t)
	tg.remove(t)

	waitForRemoved(t, collector, name)
	waitForAbsent(t, d, name)
}

func TestLabelSkip(t *testing.T) {
	d := newDiscovery(t)

	name := "it-skipped"
	startTarget(t, targetOpts{
		name:   name,
		labels: labels("dev.local", "false"),
	})

	time.Sleep(2 * time.Second)
	if infoByName(d, name) != nil {
		t.Fatalf("container %s should have been skipped", name)
	}
}

func TestComposeDetection(t *testing.T) {
	d := newDiscovery(t)

	name := "it-compose"
	startTarget(t, targetOpts{
		name: name,
		labels: labels(
			"com.docker.compose.project", "myproj",
			"com.docker.compose.service", "websvc",
		),
	})

	waitForContainers(t, d, name)
	info := infoByName(d, name)
	if info == nil {
		t.Fatalf("container %s missing", name)
	}
	if !info.IsCompose {
		t.Fatalf("expected IsCompose true, got false")
	}
	if info.Project != "myproj" {
		t.Fatalf("expected project myproj, got %q", info.Project)
	}
	if info.Service != "websvc" {
		t.Fatalf("expected service websvc, got %q", info.Service)
	}
}

func TestCustomDomains(t *testing.T) {
	d := newDiscovery(t)

	name := "it-custom"
	startTarget(t, targetOpts{
		name:   name,
		labels: labels("dev.local.domains", "80:custom.example.dev.local"),
	})

	waitForContainers(t, d, name)
	info := infoByName(d, name)
	if info == nil {
		t.Fatalf("container %s missing", name)
	}
	if len(info.CustomDomains) != 1 {
		t.Fatalf("expected 1 custom domain, got %d", len(info.CustomDomains))
	}
	if info.CustomDomains[0].Domain != "custom.example.dev.local" {
		t.Fatalf("unexpected domain %q", info.CustomDomains[0].Domain)
	}
	if info.CustomDomains[0].Port != 80 {
		t.Fatalf("unexpected port %d", info.CustomDomains[0].Port)
	}
}

func TestGatewayTargeting(t *testing.T) {
	cfg := *sharedConfig
	cfg.PollInterval = 500 * time.Millisecond
	d := newDiscoveryWithConfig(t, &cfg)

	name := "it-gateway"
	startTarget(t, targetOpts{
		name:    name,
		portMap: map[string]string{"80/tcp": ""},
	})

	waitForContainers(t, d, name)
	waitForPublishedPort(t, d, name, 80)

	info := infoByName(d, name)
	if info == nil {
		t.Fatalf("container %s missing", name)
	}
	if len(info.PublishedPorts) == 0 {
		t.Fatalf("expected at least one published port, got %v", info.PublishedPorts)
	}
	if hostPort, ok := info.PublishedPorts[80]; !ok || hostPort == 0 {
		t.Fatalf("expected published port 80 mapped to a host port, got %v", info.PublishedPorts)
	}
}
