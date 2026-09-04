package plugin

import (
	"context"
	"errors"
	"fmt"

	"github.com/jrsearles/caddy-dev-local/discovery"
)

type Sequence struct {
	name    string
	plugins []Plugin
}

func NewSequence(name string, plugins ...Plugin) *Sequence {
	return &Sequence{name: name, plugins: append([]Plugin(nil), plugins...)}
}

func (s *Sequence) Name() string {
	return s.name
}

func (s *Sequence) Apply(ctx context.Context, delta discovery.Delta) error { //nolint:gocritic
	for _, p := range s.plugins {
		pluginDelta := discovery.CloneDelta(&delta)
		if err := p.Apply(ctx, pluginDelta); err != nil {
			return fmt.Errorf("%s: %w", p.Name(), err)
		}
	}
	return nil
}

func (s *Sequence) Cleanup(ctx context.Context) error {
	var cleanupErrors []error
	for _, p := range s.plugins {
		if err := p.Cleanup(ctx); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("%s: %w", p.Name(), err))
		}
	}
	return errors.Join(cleanupErrors...)
}
