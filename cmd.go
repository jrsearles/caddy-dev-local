package caddydevlocal

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/caddyserver/caddy/v2"
	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
	"go.uber.org/zap"

	"github.com/jsearles/caddy-dev-local/config"
	"github.com/jsearles/caddy-dev-local/docker"
	"github.com/jsearles/caddy-dev-local/generator"
	"github.com/jsearles/caddy-dev-local/hosts"
)

func init() {
	caddycmd.RegisterCommand(caddycmd.Command{
		Name:  "devlocal",
		Func:  cmdFunc,
		Usage: "[flags]",
		Short: "Run caddy as a devlocal docker proxy",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("devlocal", flag.ExitOnError)

			fs.String("ingress-network", "",
				"Docker network name (env: DEVLOCAL_INGRESS_NETWORK)")

			fs.String("tld", "",
				"Top-level domain (env: DEVLOCAL_TLD)")

			fs.Duration("stale-ttl", 0,
				"Keep config for stopped containers (env: DEVLOCAL_STALE_TTL)")

			fs.Duration("probe-timeout", 0,
				"HTTP probe timeout (env: DEVLOCAL_PROBE_TIMEOUT)")

			fs.Bool("hosts-file", true,
				"Manage /etc/hosts entries for domains (env: DEVLOCAL_HOSTS_FILE)")

			fs.String("config", "",
				"Path to Caddyfile or config file (env: DEVLOCAL_CONFIG)")

			return fs
		}(),
	})

	caddycmd.RegisterCommand(caddycmd.Command{
		Name:  "devlocal-clean",
		Func:  cleanFunc,
		Usage: "",
		Short: "Remove devlocal entries from the hosts file",
	})
}

func cmdFunc(fs caddycmd.Flags) (int, error) {
	cfg := config.DefaultConfig()

	if v := fs.String("ingress-network"); v != "" {
		cfg.IngressNetwork = v
	}
	if v := fs.String("tld"); v != "" {
		cfg.TLD = v
	}
	if v := fs.Duration("stale-ttl"); v > 0 {
		cfg.StaleTTL = v
	}
	if v := fs.Duration("probe-timeout"); v > 0 {
		cfg.ProbeTimeout = v
	}

	hostsFile := fs.Bool("hosts-file")
	cfg.ApplyFlags(
		fs.String("ingress-network"),
		fs.String("tld"),
		fs.Duration("stale-ttl"),
		fs.Duration("probe-timeout"),
		&hostsFile,
	)

	configPath := fs.String("config")
	if configPath == "" {
		configPath = os.Getenv("DEVLOCAL_CONFIG")
	}
	if configPath == "" {
		configPath = detectUserConfig()
	}

	ingressExplicit := fs.String("ingress-network") != "" || os.Getenv("DEVLOCAL_INGRESS_NETWORK") != ""
	if !ingressExplicit {
		cfg.Standalone = config.DetectStandalone()
	}

	logger := caddy.Log().Named("devlocal")

	mode := "docker"
	if cfg.Standalone {
		mode = "standalone"
	}
	logger.Info("starting devlocal",
		zap.String("mode", mode),
		zap.String("tld", cfg.TLD),
		zap.String("ingress_network", cfg.IngressNetwork),
		zap.String("user_config", configPath),
	)

	hostsOK := true
	if cfg.HostsFile {
		if !hosts.CanWrite() {
			logger.Warn("hosts file not writable, skipping hosts file updates")
			hostsOK = false
		}
	}

	dockerClient, err := docker.NewClient()
	if err != nil {
		return 1, fmt.Errorf("creating docker client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gen := generator.NewGenerator(cfg, dockerClient)

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return 1, fmt.Errorf("getting cache dir: %w", err)
	}
	indexDir := filepath.Join(cacheDir, "caddy-dev-local")
	if err := os.MkdirAll(indexDir, 0755); err != nil {
		return 1, fmt.Errorf("creating index dir: %w", err)
	}

	if err := gen.RefreshAndSelect(ctx); err != nil {
		logger.Error("initial refresh failed", zap.Error(err))
	}
	logger.Info("discovered containers", zap.Int("count", len(gen.Containers())))

	api := newAdminAPI()

	if err := initCaddyConfig(gen, cfg, indexDir, api, configPath); err != nil {
		return 1, fmt.Errorf("loading initial caddy config: %w", err)
	}

	if err := syncHosts(gen, cfg, hostsOK); err != nil {
		logger.Error("failed to update hosts file", zap.Error(err))
	}

	go watchEvents(ctx, dockerClient, gen, cfg, hostsOK, indexDir, api)
	go staleCleanup(ctx, cfg, gen, hostsOK, indexDir, api)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	cancel()
	return 0, nil
}

func watchEvents(ctx context.Context, dockerClient docker.Client, gen *generator.Generator, cfg *config.Config, hostsOK bool, indexDir string, api *adminAPI) {
	logger := caddy.Log().Named("devlocal")
	logger.Info("watching docker events")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		f := client.Filters{}
		f.Add("type", "container")
		f.Add("type", "network")
		if !cfg.Standalone {
			f.Add("network", cfg.IngressNetwork)
		}

		msgCh, errCh := dockerClient.Events(ctx, client.EventsListOptions{
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
					logger.Warn("event stream closed, reconnecting in 30s")
					throttle.Stop()
					streaming = false
				} else if shouldRefresh(&event, cfg) {
					if !pending {
						pending = true
						throttle.Reset(100 * time.Millisecond)
					}
				}
			case err, ok := <-errCh:
				if !ok || err != nil {
					logger.Error("event stream error, reconnecting in 30s", zap.Error(err))
					throttle.Stop()
					streaming = false
				}
			case <-throttle.C:
				if pending {
					pending = false
					gen.RefreshAndSelect(ctx) //nolint:errcheck // non-critical, logged via logger
					if err := reloadCaddyConfig(gen, cfg, indexDir, api); err != nil {
						logger.Error("failed to reload caddy config", zap.Error(err))
					} else {
						logger.Info("reloaded config",
							zap.Int("containers", len(gen.Containers())),
							zap.Int("domains", len(gen.Domains())),
						)
					}
					if err := syncHosts(gen, cfg, hostsOK); err != nil {
						logger.Error("failed to update hosts file", zap.Error(err))
					}
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

func staleCleanup(ctx context.Context, cfg *config.Config, gen *generator.Generator, hostsOK bool, indexDir string, api *adminAPI) {
	logger := caddy.Log().Named("devlocal")
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			gen.StaleCleanup()
			logger.Debug("stale cleanup completed")
			if err := reloadCaddyConfig(gen, cfg, indexDir, api); err != nil {
				logger.Error("failed to reload caddy config", zap.Error(err))
			}
			if err := syncHosts(gen, cfg, hostsOK); err != nil {
				caddy.Log().Named("devlocal").Error("failed to update hosts file", zap.Error(err))
			}
		}
	}
}

func syncHosts(gen *generator.Generator, cfg *config.Config, hostsOK bool) error {
	if !cfg.HostsFile || !hostsOK {
		return nil
	}
	return hosts.Sync(cfg.TLD, gen.Domains())
}

func cleanFunc(fs caddycmd.Flags) (int, error) {
	logger := caddy.Log().Named("devlocal")
	if err := hosts.Remove(); err != nil {
		return 1, err
	}
	logger.Info("hosts file entries removed")
	return 0, nil
}

func detectUserConfig() string {
	candidates := []string{"Caddyfile", "Caddyfile.json", "Caddyfile.json5", "Caddyfile.yaml"}
	for _, name := range candidates {
		if _, err := os.Stat(name); err == nil {
			return name
		}
	}
	return ""
}

const (
	adapterJSON      = "json"
	adapterCaddyfile = "caddyfile"
)

func adapterFor(configPath string) string {
	switch strings.ToLower(filepath.Ext(configPath)) {
	case ".json":
		return adapterJSON
	case ".json5":
		return "json5"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return adapterCaddyfile
	}
}
