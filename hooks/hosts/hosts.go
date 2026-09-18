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

type Hook struct {
	cfg      *config.Config
	enabled  bool
	writable bool
	sync     func(string, []string) error
	remove   func() error
	mu       sync.Mutex
}

func New(cfg *config.Config, enabled, writable bool) *Hook {
	return &Hook{
		cfg:      cfg,
		enabled:  enabled,
		writable: writable,
		sync:     hostsfile.Sync,
		remove:   hostsfile.Remove,
	}
}

func (p *Hook) Name() string {
	return "hosts"
}

func (p *Hook) Apply(_ context.Context, update discovery.Update) error { //nolint:gocritic
	if !p.enabled || !p.writable {
		return nil
	}
	if p.cfg == nil {
		return fmt.Errorf("hosts hook: config is nil")
	}
	if update.Status.LastRefresh.IsZero() && update.Status.LastError != "" {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sync(p.cfg.TLD, generator.Domains(p.cfg, update.Snapshot))
}

func (p *Hook) Cleanup(context.Context) error {
	if !p.enabled {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.remove()
}
