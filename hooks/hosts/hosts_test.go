package hosts

import (
	"context"
	"reflect"
	"testing"

	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
)

func TestApplyReconcilesSnapshotDomains(t *testing.T) {
	p := New(&config.Config{TLD: "dev.local"}, true, true)
	var gotTLD string
	var gotDomains []string
	p.sync = func(tld string, domains []string) error {
		gotTLD = tld
		gotDomains = domains
		return nil
	}

	update := discovery.Update{Snapshot: []*discovery.ContainerInfo{
		{ContainerName: "web", IsRunning: true, Ports: []uint16{80}, PublishedPorts: map[uint16]uint16{80: 8080}},
		{ContainerName: "stopped", IsRunning: false, Ports: []uint16{80}},
	}}
	if err := p.Apply(context.Background(), update); err != nil {
		t.Fatal(err)
	}

	want := []string{"web.dev.local", "web.localhost"}
	if gotTLD != "dev.local" || !reflect.DeepEqual(gotDomains, want) {
		t.Fatalf("sync = (%q, %v), want (%q, %v)", gotTLD, gotDomains, "dev.local", want)
	}
}

func TestApplySkipsDisabledOrUnwritable(t *testing.T) {
	for _, states := range [][2]bool{{false, true}, {true, false}} {
		p := New(&config.Config{TLD: "dev.local"}, states[0], states[1])
		p.sync = func(string, []string) error {
			t.Fatal("sync called")
			return nil
		}
		if err := p.Apply(context.Background(), discovery.Update{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCleanupRemovesManagedBlock(t *testing.T) {
	p := New(&config.Config{}, true, true)
	called := false
	p.remove = func() error {
		called = true
		return nil
	}
	if err := p.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("hosts cleanup was not called")
	}
}
