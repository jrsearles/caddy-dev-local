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

	"github.com/caddyserver/caddy/v2"
	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	"go.uber.org/zap"

	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
	"github.com/jrsearles/caddy-dev-local/docker"
	"github.com/jrsearles/caddy-dev-local/generator"
	"github.com/jrsearles/caddy-dev-local/hosts"
)

func init() {
	caddycmd.RegisterCommand(caddycmd.Command{
		Name:  appName,
		Func:  cmdFunc,
		Usage: "[flags]",
		Short: "Run caddy as a devlocal docker proxy",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet(appName, flag.ExitOnError)

			config.RegisterSharedGoFlags(fs)

			fs.Bool("hosts-file", true,
				"Manage /etc/hosts entries for domains (env: DEVLOCAL_HOSTS_FILE)")

			fs.Bool("no-tracing", false,
				"Disable OpenTelemetry tracing on dynamic routes (env: DEVLOCAL_TRACING)")

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
	applyCommandFlags(cfg, fs)

	configPath := fs.String("config")
	if configPath == "" {
		configPath = os.Getenv("DEVLOCAL_CONFIG")
	}
	if configPath == "" {
		configPath = detectUserConfig()
	}

	logger := caddy.Log().Named(appName)

	logger.Info("starting devlocal",
		zap.String("tld", cfg.TLD),
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

	disc := discovery.New(cfg, dockerClient, logger)
	disc.SetEnricher(generator.PortSelector(cfg, generator.ProbeHTTPPort))

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return 1, fmt.Errorf("getting cache dir: %w", err)
	}
	indexDir := filepath.Join(cacheDir, "caddy-dev-local")
	if err := os.MkdirAll(indexDir, 0755); err != nil {
		return 1, fmt.Errorf("creating index dir: %w", err)
	}

	if err := disc.Refresh(ctx); err != nil {
		logger.Error("initial refresh failed", zap.Error(err))
	}
	logger.Info("discovered containers", zap.Int("count", len(disc.Snapshot())))

	api := newAdminAPI()

	if err := initCaddyConfig(cfg, indexDir, api, configPath, disc); err != nil {
		return 1, fmt.Errorf("loading initial caddy config: %w", err)
	}

	if err := syncHosts(cfg, hostsOK, disc); err != nil {
		logger.Error("failed to update hosts file", zap.Error(err))
	}

	apply := func() { applyDevlocal(cfg, indexDir, api, hostsOK, disc) }
	disc.Subscribe(func(discovery.Delta) { apply() })
	disc.Run(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	cancel()
	return 0, nil
}

func applyCommandFlags(cfg *config.Config, fs caddycmd.Flags) {
	o := config.SharedOverrides(fs.FlagSet)
	if fs.Changed("hosts-file") {
		v := fs.Bool("hosts-file")
		o.HostsFile = &v
	}
	if fs.Changed("no-tracing") {
		v := fs.Bool("no-tracing")
		f := !v
		o.Tracing = &f
	}
	cfg.ApplyFlags(o)
}

func applyDevlocal(cfg *config.Config, indexDir string, api *adminAPI, hostsOK bool, disc *discovery.Discovery) {
	logger := caddy.Log().Named(appName)
	if !api.tryBeginApply() {
		logger.Debug("apply in flight, skipping")
		return
	}
	defer api.endApply()

	applied, err := reloadCaddyConfig(cfg, indexDir, api, disc)
	if err != nil {
		logger.Error("failed to reload caddy config", zap.Error(err))
	} else if applied {
		logger.Info("reloaded config",
			zap.Int("containers", len(disc.Snapshot())),
			zap.Int("domains", len(generator.Domains(cfg, disc.Snapshot()))),
		)
	}
	if err := syncHosts(cfg, hostsOK, disc); err != nil {
		logger.Error("failed to update hosts file", zap.Error(err))
	}
}

func syncHosts(cfg *config.Config, hostsOK bool, disc *discovery.Discovery) error {
	if !cfg.HostsFile || !hostsOK {
		return nil
	}
	return hosts.Sync(cfg.TLD, generator.Domains(cfg, disc.Snapshot()))
}

func cleanFunc(fs caddycmd.Flags) (int, error) {
	logger := caddy.Log().Named(appName)
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
	appName          = "devlocal"
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
