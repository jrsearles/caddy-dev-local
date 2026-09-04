package hosts

import (
	"context"
	"fmt"
	"sync"

	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
	"github.com/jrsearles/caddy-dev-local/generator"
	hostsfile "github.com/jrsearles/caddy-dev-local/hosts"
)

type Plugin struct {
	cfg      *config.Config
	enabled  bool
	writable bool
	sync     func(string, []string) error
	remove   func() error
	mu       sync.Mutex
}

func New(cfg *config.Config, enabled, writable bool) *Plugin {
	return &Plugin{
		cfg:      cfg,
		enabled:  enabled,
		writable: writable,
		sync:     hostsfile.Sync,
		remove:   hostsfile.Remove,
	}
}

func (p *Plugin) Name() string {
	return "hosts"
}

func (p *Plugin) Apply(_ context.Context, delta discovery.Delta) error { //nolint:gocritic
	if !p.enabled || !p.writable {
		return nil
	}
	if p.cfg == nil {
		return fmt.Errorf("hosts plugin: config is nil")
	}
	if delta.Status.LastRefresh.IsZero() && delta.Status.LastError != "" {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sync(p.cfg.TLD, generator.Domains(p.cfg, delta.Snapshot))
}

func (p *Plugin) Cleanup(context.Context) error {
	if !p.enabled {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.remove()
}
