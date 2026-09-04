package controller

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/jrsearles/caddy-dev-local/caddyapi"
	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
	dockerclient "github.com/jrsearles/caddy-dev-local/docker"
	"github.com/jrsearles/caddy-dev-local/generator"
	"github.com/jrsearles/caddy-dev-local/hosts"
	"github.com/jrsearles/caddy-dev-local/plugin"
	caddyplugin "github.com/jrsearles/caddy-dev-local/plugins/caddy"
	hostsplugin "github.com/jrsearles/caddy-dev-local/plugins/hosts"
	uiplugin "github.com/jrsearles/caddy-dev-local/plugins/ui"
)

type Options struct {
	Config       *config.Config
	DockerClient dockerclient.Client
	AdminClient  *caddyapi.Client
	Logger       *zap.Logger
	IndexDir     string
	Caddy        bool
	UI           bool
	Hosts        bool
}

func Run(ctx context.Context, options Options) error {
	if options.Config == nil {
		return fmt.Errorf("controller config is nil")
	}
	if options.AdminClient == nil && (options.Caddy || options.UI) {
		return fmt.Errorf("caddy API client is required when Caddy or UI is enabled")
	}
	if options.Logger == nil {
		options.Logger = zap.NewNop()
	}
	if options.DockerClient == nil {
		client, err := dockerclient.NewClient()
		if err != nil {
			return fmt.Errorf("creating Docker client: %w", err)
		}
		options.DockerClient = client
	}

	disc := discovery.New(options.Config, options.DockerClient, options.Logger)
	if options.Caddy || options.UI {
		disc.AddEnricher(generator.PortSelector(options.Config, generator.ProbeHTTPPort))
	}

	runtime, err := newRuntime(options)
	if err != nil {
		return err
	}

	if err := disc.Refresh(ctx); err != nil {
		options.Logger.Error("initial discovery refresh failed", zap.Error(err))
	}
	initial := discovery.Delta{Snapshot: disc.Snapshot(), Status: disc.Status()}
	if err := runtime.Start(ctx, initial); err != nil {
		return err
	}
	disc.Subscribe(runtime.Submit)
	disc.Run(ctx)
	options.Logger.Info("discovery started", zap.Int("containers", len(initial.Snapshot)))

	<-ctx.Done()
	return runtime.Wait()
}

func Cleanup(ctx context.Context, options Options) error {
	if options.Config == nil {
		return fmt.Errorf("controller config is nil")
	}
	if options.AdminClient == nil && options.Caddy {
		return fmt.Errorf("caddy API client is required when Caddy is enabled")
	}
	if options.Logger == nil {
		options.Logger = zap.NewNop()
	}
	runtime, err := newRuntime(options)
	if err != nil {
		return err
	}
	return runtime.Cleanup(ctx)
}

func newRuntime(options Options) (*plugin.Runtime, error) {
	runtime := plugin.NewRuntime(options.Logger)
	switch {
	case options.Caddy && options.UI:
		caddy := caddyplugin.New(options.Config, options.IndexDir, options.AdminClient)
		ui := uiplugin.New(options.Config.TLD, options.IndexDir, options.AdminClient)
		if err := runtime.Register(plugin.NewSequence("caddy-ui", caddy, ui)); err != nil {
			return nil, err
		}
	case options.Caddy:
		if err := runtime.Register(caddyplugin.New(options.Config, "", options.AdminClient)); err != nil {
			return nil, err
		}
	case options.UI:
		if err := runtime.Register(uiplugin.New(options.Config.TLD, options.IndexDir, options.AdminClient)); err != nil {
			return nil, err
		}
	}
	if options.Hosts {
		writable := hosts.CanWrite()
		if !writable {
			options.Logger.Warn("hosts file not writable, skipping hosts plugin")
		}
		if err := runtime.Register(hostsplugin.New(options.Config, true, writable)); err != nil {
			return nil, err
		}
	}
	return runtime, nil
}
