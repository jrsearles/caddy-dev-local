//go:build integration

package integration

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/jrsearles/caddy-dev-local/discovery"
)

const targetImage = "traefik/whoami"

type targetOpts struct {
	name       string
	labels     map[string]string
	exposed    []string
	networks   []string
	portMap    map[string]string
	startedNow bool
}

type target struct {
	c    testcontainers.Container
	name string
}

func startTarget(t *testing.T, o targetOpts) target {
	t.Helper()

	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        targetImage,
		Name:         o.name,
		Labels:       o.labels,
		ExposedPorts: o.exposed,
		Networks:     o.networks,
		WaitingFor:   wait.ForLog("Starting up on"),
	}
	if len(o.portMap) > 0 {
		req.ExposedPorts = []string{"80/tcp"}
		req.HostConfigModifier = func(hc *container.HostConfig) {
			hc.PortBindings = network.PortMap{}
			for containerPort, hostPort := range o.portMap {
				p, err := network.ParsePort(containerPort)
				if err != nil {
					panic(err)
				}
				hc.PortBindings[network.Port(p)] = []network.PortBinding{
					{HostIP: netip.MustParseAddr("0.0.0.0"), HostPort: hostPort},
				}
			}
		}
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("starting target container %s: %v", o.name, err)
	}

	t.Cleanup(func() {
		_ = c.Terminate(context.Background())
	})

	return target{c: c, name: o.name}
}

func (tg target) start(t *testing.T) {
	t.Helper()
	if err := tg.c.Start(context.Background()); err != nil {
		t.Fatalf("starting container %s: %v", tg.name, err)
	}
}

func (tg target) stop(t *testing.T) {
	t.Helper()
	if err := tg.c.Stop(context.Background(), nil); err != nil {
		t.Fatalf("stopping container %s: %v", tg.name, err)
	}
}

func (tg target) remove(t *testing.T) {
	t.Helper()
	if err := tg.c.Terminate(context.Background()); err != nil {
		t.Fatalf("removing container %s: %v", tg.name, err)
	}
}

func (tg target) forceRemove(t *testing.T) {
	t.Helper()
	id := tg.c.GetContainerID()
	c, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		t.Fatalf("creating docker client: %v", err)
	}
	defer c.Close()
	if _, err := c.ContainerRemove(context.Background(), id, mobyclient.ContainerRemoveOptions{Force: true}); err != nil {
		t.Fatalf("force-removing container %s: %v", tg.name, err)
	}
}

// namesInSnapshot returns the set of container names currently discovered.
func namesInSnapshot(d *discovery.Discovery) map[string]bool {
	names := make(map[string]bool)
	for _, info := range d.Snapshot() {
		names[info.ContainerName] = true
	}
	return names
}

func infoByName(d *discovery.Discovery, name string) *discovery.ContainerInfo {
	for _, info := range d.Snapshot() {
		if info.ContainerName == name {
			return info
		}
	}
	return nil
}

// waitForContainers blocks until the discovery snapshot contains all the given
// names. The host daemon may hold unrelated containers, so only the wanted
// subset is required.
func waitForContainers(t *testing.T, d *discovery.Discovery, want ...string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got := namesInSnapshot(d)
		present := true
		for _, n := range want {
			if !got[n] {
				present = false
			}
		}
		if present {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for snapshot to contain %v; got %v", want, namesInSnapshot(d))
}

// waitForPublishedPort blocks until the discovered container for name has a
// non-zero public port published under the given private port. The host port is
// auto-assigned and unknown in advance, so only non-zero is required. Docker may
// briefly report a freshly-started container before the port binding is
// populated, so callers must wait rather than assert immediately.
func waitForPublishedPort(t *testing.T, d *discovery.Discovery, name string, private uint16) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		info := infoByName(d, name)
		if info != nil {
			if got, ok := info.PublishedPorts[private]; ok && got != 0 {
				return
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q to publish port %d; got %v",
		name, private, infoByName(d, name).PublishedPorts)
}

// waitForAbsent blocks until the discovery snapshot no longer contains the
// given name.
func waitForAbsent(t *testing.T, d *discovery.Discovery, name string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if !namesInSnapshot(d)[name] {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q to leave snapshot; got %v", name, namesInSnapshot(d))
}

func waitForStopped(t *testing.T, d *discovery.Discovery, name string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if info := infoByName(d, name); info != nil && !info.IsRunning {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q to stop", name)
}

func labels(kv ...string) map[string]string {
	m := make(map[string]string)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}
