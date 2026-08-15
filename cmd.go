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

	"github.com/jsearles/caddy-dev-local/config"
	"github.com/jsearles/caddy-dev-local/discovery"
	"github.com/jsearles/caddy-dev-local/docker"
	"github.com/jsearles/caddy-dev-local/generator"
	"github.com/jsearles/caddy-dev-local/hosts"
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

	config.ResolveStandalone(cfg)

	logger := caddy.Log().Named(appName)

	mode := "docker"
	if cfg.Standalone {
		mode = "standalone"
	}
	logger.Info("starting devlocal",
		zap.String("mode", mode),
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

	apply := func() { applyDevlocal(gen, cfg, indexDir, api, hostsOK) }
	discovery.New(cfg, dockerClient, gen, gen.RefreshAndSelect, apply, logger).Run(ctx)

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
	cfg.ApplyFlags(o)
}

func applyDevlocal(gen *generator.Generator, cfg *config.Config, indexDir string, api *adminAPI, hostsOK bool) {
	logger := caddy.Log().Named(appName)
	if !api.tryBeginApply() {
		logger.Debug("apply in flight, skipping")
		return
	}
	defer api.endApply()

	applied, err := reloadCaddyConfig(gen, cfg, indexDir, api)
	if err != nil {
		logger.Error("failed to reload caddy config", zap.Error(err))
	} else if applied {
		logger.Info("reloaded config",
			zap.Int("containers", len(gen.Containers())),
			zap.Int("domains", len(gen.Domains())),
		)
	}
	if err := syncHosts(gen, cfg, hostsOK); err != nil {
		logger.Error("failed to update hosts file", zap.Error(err))
	}
}

func syncHosts(gen *generator.Generator, cfg *config.Config, hostsOK bool) error {
	if !cfg.HostsFile || !hostsOK {
		return nil
	}
	return hosts.Sync(cfg.TLD, gen.Domains())
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
