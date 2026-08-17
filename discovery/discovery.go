package discovery

import (
	"context"
	"sync"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
	"go.uber.org/zap"

	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/docker"
	"github.com/jrsearles/caddy-dev-local/generator"
)

type Status struct {
	LastError   string
	LastErrorAt time.Time
	LastRefresh time.Time
}

type Controller struct {
	cfg     *config.Config
	docker  docker.Client
	gen     *generator.Generator
	refresh func(context.Context) error
	apply   func()
	logger  *zap.Logger

	mu     sync.RWMutex
	status Status
}

func New(cfg *config.Config, dockerClient docker.Client, gen *generator.Generator, refresh func(context.Context) error, apply func(), logger *zap.Logger) *Controller {
	return &Controller{
		cfg:     cfg,
		docker:  dockerClient,
		gen:     gen,
		refresh: refresh,
		apply:   apply,
		logger:  logger,
	}
}

func (c *Controller) SetApply(fn func()) {
	c.apply = fn
}

func (c *Controller) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *Controller) setError(msg string) {
	c.mu.Lock()
	c.status.LastError = msg
	c.status.LastErrorAt = time.Now()
	c.mu.Unlock()
}

func (c *Controller) clearError() {
	c.mu.Lock()
	c.status.LastError = ""
	c.status.LastErrorAt = time.Time{}
	c.status.LastRefresh = time.Now()
	c.mu.Unlock()
}

func (c *Controller) runRefresh(ctx context.Context) {
	if err := c.refresh(ctx); err != nil {
		c.setError(err.Error())
	} else {
		c.clearError()
	}
}

func (c *Controller) Run(ctx context.Context) {
	go c.watchEvents(ctx)
	go c.staleCleanup(ctx)
	if c.cfg.PollInterval > 0 {
		go c.pollLoop(ctx)
	}
}

func (c *Controller) watchEvents(ctx context.Context) {
	c.logger.Info("watching docker events")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		f := client.Filters{}
		f.Add("type", "container")
		f.Add("type", "network")

		msgCh, errCh := c.docker.Events(ctx, client.EventsListOptions{
			Filters: f,
		})

		throttle := time.NewTimer(100 * time.Millisecond)
		pending := false

		streaming := true
		for streaming {
			select {
			case <-ctx.Done():
				throttle.Stop()
				return
			case event, ok := <-msgCh:
				if !ok {
					c.logger.Warn("event stream closed, reconnecting in 30s")
					c.setError("Docker event stream closed, reconnecting")
					throttle.Stop()
					streaming = false
				} else if shouldRefresh(&event) {
					if !pending {
						pending = true
						throttle.Reset(100 * time.Millisecond)
					}
				}
			case err, ok := <-errCh:
				if !ok || err != nil {
					c.logger.Error("event stream error, reconnecting in 30s", zap.Error(err))
					c.setError("Docker event stream error: " + err.Error())
					throttle.Stop()
					streaming = false
				}
			case <-throttle.C:
				if pending {
					pending = false
					c.runRefresh(ctx)
					c.apply()
				}
			}
		}

		throttle.Stop()
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

func shouldRefresh(event *events.Message) bool {
	switch event.Type {
	case events.ContainerEventType:
		switch event.Action {
		case events.ActionStart, events.ActionStop, events.ActionDie, events.ActionDestroy, events.ActionCreate:
			return true
		default:
			return false
		}
	case events.NetworkEventType:
		switch event.Action {
		case events.ActionConnect, events.ActionDisconnect:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func (c *Controller) staleCleanup(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.gen.StaleCleanup()
			c.logger.Debug("stale cleanup completed")
			c.apply()
		}
	}
}

func (c *Controller) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.runRefresh(ctx)
			c.apply()
		}
	}
}
