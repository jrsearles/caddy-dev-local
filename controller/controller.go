package controller

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jrsearles/caddy-dev-local/caddyapi"
	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
	dockerclient "github.com/jrsearles/caddy-dev-local/docker"
	"github.com/jrsearles/caddy-dev-local/hook"
	caddyhook "github.com/jrsearles/caddy-dev-local/hooks/caddy"
	hostshook "github.com/jrsearles/caddy-dev-local/hooks/hosts"
	uihook "github.com/jrsearles/caddy-dev-local/hooks/ui"
	"github.com/jrsearles/caddy-dev-local/hosts"
)

type Options struct {
	Config       *config.Config
	DockerClient dockerclient.Client
	AdminClient  *caddyapi.Client
	Logger       *slog.Logger
	IndexDir     string
	Caddy        bool
	UI           bool
	Hosts        bool
}

func Run(ctx context.Context, options Options) error {
	options, disc, runtime, err := prepare(options)
	if err != nil {
		return err
	}

	disc.Run(ctx)
	updates := disc.Updates()
	var initial discovery.Update
	var ok bool
	select {
	case initial, ok = <-updates:
	case <-ctx.Done():
	}
	if !ok {
		return fmt.Errorf("discovery stopped before publishing initial state")
	}
	if err := runtime.Start(ctx, initial); err != nil {
		return err
	}
	options.Logger.Info("discovery started", slog.Int("containers", len(initial.Snapshot)))
	go func() {
		for {
			select {
			case update, open := <-updates:
				if !open {
					return
				}
				runtime.Submit(update)
			case <-ctx.Done():
				return
			}
		}
	}()

	<-ctx.Done()
	return runtime.Wait()
}

// RunOnce discovers the current Docker state and synchronously reconciles it.
func RunOnce(ctx context.Context, options Options) error {
	options, disc, runtime, err := prepare(options)
	if err != nil {
		return err
	}
	if err := disc.Refresh(ctx); err != nil {
		return err
	}
	update := discovery.Update{Snapshot: disc.Snapshot(), Status: disc.Status()}
	if err := runtime.ApplyOnce(ctx, update); err != nil {
		return err
	}
	options.Logger.Info("one-pass reconciliation complete; run devlocal start to watch continuously")
	return nil
}

func Cleanup(ctx context.Context, options Options) error {
	if options.Config == nil {
		return fmt.Errorf("controller config is nil")
	}
	if options.AdminClient == nil && options.Caddy {
		return fmt.Errorf("caddy API client is required when Caddy is enabled")
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	runtime, err := newRuntime(options)
	if err != nil {
		return err
	}
	return runtime.Cleanup(ctx)
}

func prepare(options Options) (Options, *discovery.Discovery, *hook.Runtime, error) {
	if options.Config == nil {
		return options, nil, nil, fmt.Errorf("controller config is nil")
	}
	if options.AdminClient == nil && (options.Caddy || options.UI) {
		return options, nil, nil, fmt.Errorf("caddy API client is required when Caddy or UI is enabled")
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	if options.DockerClient == nil {
		client, err := dockerclient.NewClient()
		if err != nil {
			return options, nil, nil, fmt.Errorf("creating Docker client: %w", err)
		}
		options.DockerClient = client
	}

	var discoveryOptions []discovery.Option
	if options.Caddy || options.UI {
		discoveryOptions = append(discoveryOptions, discovery.WithPortProbe(discovery.ProbeHTTPPort))
	}
	disc := discovery.New(options.Config, options.DockerClient, options.Logger, discoveryOptions...)
	runtime, err := newRuntime(options)
	if err != nil {
		return options, nil, nil, err
	}
	return options, disc, runtime, nil
}

func newRuntime(options Options) (*hook.Runtime, error) {
	runtime := hook.NewRuntime(options.Logger)
	switch {
	case options.Caddy && options.UI:
		caddy := caddyhook.New(options.Config, options.AdminClient)
		ui := uihook.New(options.Config, options.IndexDir, options.AdminClient)
		if err := runtime.Register(hook.NewSequence("caddy-ui", caddy, ui)); err != nil {
			return nil, err
		}
	case options.Caddy:
		if err := runtime.Register(caddyhook.New(options.Config, options.AdminClient)); err != nil {
			return nil, err
		}
	case options.UI:
		if err := runtime.Register(uihook.New(options.Config, options.IndexDir, options.AdminClient)); err != nil {
			return nil, err
		}
	}
	if options.Hosts {
		writable := hosts.CanWrite()
		if !writable {
			options.Logger.Warn("hosts file not writable, skipping hosts hook")
		}
		if err := runtime.Register(hostshook.New(options.Config, true, writable)); err != nil {
			return nil, err
		}
	}
	return runtime, nil
}
