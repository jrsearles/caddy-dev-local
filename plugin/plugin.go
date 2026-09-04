package plugin

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/jrsearles/caddy-dev-local/discovery"
)

type Plugin interface {
	Name() string
	Apply(context.Context, discovery.Delta) error
	Cleanup(context.Context) error
}

type Func struct {
	PluginName  string
	ApplyFunc   func(context.Context, discovery.Delta) error
	CleanupFunc func(context.Context) error
}

func (f Func) Name() string {
	return f.PluginName
}

func (f Func) Apply(ctx context.Context, delta discovery.Delta) error { //nolint:gocritic
	if f.ApplyFunc == nil {
		return errors.New("plugin apply function is nil")
	}
	return f.ApplyFunc(ctx, delta)
}

func (f Func) Cleanup(ctx context.Context) error {
	if f.CleanupFunc == nil {
		return nil
	}
	return f.CleanupFunc(ctx)
}

type Runtime struct {
	logger *zap.Logger

	mu      sync.Mutex
	plugins []Plugin
	workers []worker
	ctx     context.Context
	started bool
	done    chan struct{}
}

type worker struct {
	plugin  Plugin
	pending chan discovery.Delta
}

func NewRuntime(logger *zap.Logger) *Runtime {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Runtime{logger: logger}
}

func (r *Runtime) Register(p Plugin) error {
	if p == nil {
		return errors.New("plugin is nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return errors.New("plugin runtime already started")
	}
	r.plugins = append(r.plugins, p)
	return nil
}

// Submit queues delta for every plugin without waiting for plugin execution.
// If a plugin already has pending work, that work is replaced by delta.
func (r *Runtime) Submit(delta discovery.Delta) { //nolint:gocritic
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started || r.ctx.Err() != nil {
		return
	}

	for _, w := range r.workers {
		pluginDelta := discovery.CloneDelta(&delta)
		select {
		case w.pending <- pluginDelta:
			continue
		default:
		}
		select {
		case <-w.pending:
		default:
		}
		select {
		case w.pending <- pluginDelta:
		default:
		}
	}
}

// Start starts the registered plugins. Each plugin receives initial before any
// delta passed to Submit.
func (r *Runtime) Start(ctx context.Context, initial discovery.Delta) error { //nolint:gocritic
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return errors.New("plugin runtime already started")
	}
	r.started = true
	r.ctx = ctx
	r.done = make(chan struct{})
	r.workers = make([]worker, len(r.plugins))
	for i, p := range r.plugins {
		r.workers[i] = worker{plugin: p, pending: make(chan discovery.Delta, 1)}
	}
	workers := r.workers
	r.mu.Unlock()

	var initialErrors []error
	for _, w := range workers {
		workerInitial := discovery.CloneDelta(&initial)
		if err := r.apply(ctx, w.plugin, workerInitial); err != nil {
			initialErrors = append(initialErrors, fmt.Errorf("%s: %w", w.plugin.Name(), err))
		}
	}
	if len(initialErrors) > 0 {
		close(r.done)
		return errors.Join(initialErrors...)
	}

	go func() {
		var wg sync.WaitGroup
		wg.Add(len(workers))
		for _, w := range workers {
			go func() {
				defer wg.Done()
				for ctx.Err() == nil {
					select {
					case <-ctx.Done():
						return
					case delta := <-w.pending:
						_ = r.apply(ctx, w.plugin, delta)
					}
				}
			}()
		}
		wg.Wait()
		close(r.done)
	}()
	return nil
}

// Run starts the registered plugins and blocks until they stop.
func (r *Runtime) Run(ctx context.Context, initial discovery.Delta) error { //nolint:gocritic
	if err := r.Start(ctx, initial); err != nil {
		return err
	}
	return r.Wait()
}

func (r *Runtime) Wait() error {
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	if done == nil {
		return errors.New("plugin runtime is not started")
	}
	<-done
	return nil
}

func (r *Runtime) Cleanup(ctx context.Context) error {
	r.mu.Lock()
	plugins := append([]Plugin(nil), r.plugins...)
	r.mu.Unlock()

	var cleanupErrors []error
	for _, p := range plugins {
		if err := p.Cleanup(ctx); err != nil {
			r.logger.Error("plugin cleanup failed", zap.String("plugin", p.Name()), zap.Error(err))
			cleanupErrors = append(cleanupErrors, fmt.Errorf("%s: %w", p.Name(), err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func (r *Runtime) apply(ctx context.Context, p Plugin, delta discovery.Delta) error { //nolint:gocritic
	if err := p.Apply(ctx, delta); err != nil {
		r.logger.Error("plugin apply failed", zap.String("plugin", p.Name()), zap.Error(err))
		return err
	}
	return nil
}
