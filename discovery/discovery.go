package discovery

import (
	"context"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
	"go.uber.org/zap"

	"github.com/jsearles/caddy-dev-local/config"
	"github.com/jsearles/caddy-dev-local/docker"
	"github.com/jsearles/caddy-dev-local/generator"
)

type Controller struct {
	cfg     *config.Config
	docker  docker.Client
	gen     *generator.Generator
	refresh func(context.Context) error
	apply   func()
	logger  *zap.Logger
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
		if !c.cfg.Standalone {
			f.Add("network", c.cfg.IngressNetwork)
		}

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
					throttle.Stop()
					streaming = false
				} else if shouldRefresh(&event, c.cfg) {
					if !pending {
						pending = true
						throttle.Reset(100 * time.Millisecond)
					}
				}
			case err, ok := <-errCh:
				if !ok || err != nil {
					c.logger.Error("event stream error, reconnecting in 30s", zap.Error(err))
					throttle.Stop()
					streaming = false
				}
			case <-throttle.C:
				if pending {
					pending = false
					c.refresh(ctx) //nolint:errcheck // non-critical
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

func shouldRefresh(event *events.Message, cfg *config.Config) bool {
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
			return event.Actor.Attributes["name"] == cfg.IngressNetwork
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
			c.refresh(ctx) //nolint:errcheck // non-critical
			c.apply()
		}
	}
}
