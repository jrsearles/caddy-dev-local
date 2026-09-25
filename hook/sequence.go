package hook

import (
	"context"
	"errors"
	"fmt"

	"github.com/jrsearles/caddy-dev-local/discovery"
)

type Sequence struct {
	name  string
	hooks []Hook
}

func NewSequence(name string, hooks ...Hook) *Sequence {
	return &Sequence{name: name, hooks: append([]Hook(nil), hooks...)}
}

func (s *Sequence) Name() string {
	return s.name
}

func (s *Sequence) Apply(ctx context.Context, update discovery.Update) error { //nolint:gocritic
	return s.applyEach(func(h Hook) error {
		return h.Apply(ctx, update)
	})
}

func (s *Sequence) applyEach(apply func(Hook) error) error {
	for _, h := range s.hooks {
		if err := apply(h); err != nil {
			return fmt.Errorf("%s: %w", h.Name(), err)
		}
	}
	return nil
}

func (s *Sequence) Cleanup(ctx context.Context) error {
	var cleanupErrors []error
	for _, h := range s.hooks {
		if err := h.Cleanup(ctx); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("%s: %w", h.Name(), err))
		}
	}
	return errors.Join(cleanupErrors...)
}
