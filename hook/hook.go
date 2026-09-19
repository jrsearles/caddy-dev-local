package hook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jrsearles/caddy-dev-local/discovery"
)

type Hook interface {
	Name() string
	Apply(context.Context, discovery.Update) error
	Cleanup(context.Context) error
}

type Func struct {
	HookName    string
	ApplyFunc   func(context.Context, discovery.Update) error
	CleanupFunc func(context.Context) error
}

func (f Func) Name() string {
	return f.HookName
}

func (f Func) Apply(ctx context.Context, update discovery.Update) error { //nolint:gocritic
	if f.ApplyFunc == nil {
		return errors.New("hook apply function is nil")
	}
	return f.ApplyFunc(ctx, update)
}

func (f Func) Cleanup(ctx context.Context) error {
	if f.CleanupFunc == nil {
		return nil
	}
	return f.CleanupFunc(ctx)
}

type Runtime struct {
	logger *slog.Logger

	mu      sync.Mutex
	hooks   []Hook
	workers []worker
	ctx     context.Context
	started bool
	done    chan struct{}
}

type worker struct {
	hook    Hook
	pending chan discovery.Update
}

func NewRuntime(logger *slog.Logger) *Runtime {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Runtime{logger: logger}
}

func (r *Runtime) Register(h Hook) error {
	if h == nil {
		return errors.New("hook is nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return errors.New("hook runtime already started")
	}
	r.hooks = append(r.hooks, h)
	return nil
}

// Submit queues an update for every hook without waiting for hook execution.
// If a hook already has pending work, that work is replaced by the update.
func (r *Runtime) Submit(update discovery.Update) { //nolint:gocritic
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started || r.ctx.Err() != nil {
		return
	}

	for _, w := range r.workers {
		select {
		case w.pending <- update:
			continue
		default:
		}
		select {
		case <-w.pending:
		default:
		}
		select {
		case w.pending <- update:
		default:
		}
	}
}

// Start starts the registered hooks. Each hook receives initial before any
// update passed to Submit.
func (r *Runtime) Start(ctx context.Context, initial discovery.Update) error { //nolint:gocritic
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return errors.New("hook runtime already started")
	}
	r.started = true
	r.ctx = ctx
	r.done = make(chan struct{})
	r.workers = make([]worker, len(r.hooks))
	for i, h := range r.hooks {
		r.workers[i] = worker{hook: h, pending: make(chan discovery.Update, 1)}
	}
	workers := r.workers
	r.mu.Unlock()

	var initialErrors []error
	for _, w := range workers {
		if err := r.apply(ctx, w.hook, initial); err != nil {
			initialErrors = append(initialErrors, fmt.Errorf("%s: %w", w.hook.Name(), err))
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
			r.logger.Info("hook started", slog.String("hook", w.hook.Name()))
			go func() {
				defer wg.Done()
				for ctx.Err() == nil {
					select {
					case <-ctx.Done():
						return
					case update := <-w.pending:
						_ = r.apply(ctx, w.hook, update)
					}
				}
			}()
		}
		wg.Wait()
		close(r.done)
	}()
	return nil
}

// Run starts the registered hooks and blocks until they stop.
func (r *Runtime) Run(ctx context.Context, initial discovery.Update) error { //nolint:gocritic
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
		return errors.New("hook runtime is not started")
	}
	<-done
	return nil
}

func (r *Runtime) Cleanup(ctx context.Context) error {
	r.mu.Lock()
	hooks := append([]Hook(nil), r.hooks...)
	r.mu.Unlock()

	var cleanupErrors []error
	for _, h := range hooks {
		if err := h.Cleanup(ctx); err != nil {
			r.logger.Error("hook cleanup failed", slog.String("hook", h.Name()), slog.Any("error", err))
			cleanupErrors = append(cleanupErrors, fmt.Errorf("%s: %w", h.Name(), err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func (r *Runtime) apply(ctx context.Context, h Hook, update discovery.Update) error { //nolint:gocritic
	if err := h.Apply(ctx, update); err != nil {
		r.logger.Error("hook apply failed", slog.String("hook", h.Name()), slog.Any("error", err))
		return err
	}
	return nil
}
