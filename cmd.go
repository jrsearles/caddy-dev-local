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

	"github.com/jrsearles/caddy-dev-local/caddyapi"
	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/controller"
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
			fs.Bool("ui", true, "Generate and register the index UI")
			fs.String("caddy-admin", "http://localhost:2019", "Caddy admin API URL")
			fs.String("caddy-server", "srv0", "Caddy HTTP server name")
			fs.String("index-dir", "", "Directory for generated UI files")

			fs.String("config", "",
				"Path to Caddyfile or config file (env: DEVLOCAL_CONFIG)")

			return fs
		}(),
	})

	caddycmd.RegisterCommand(caddycmd.Command{
		Name:  "devlocal-clean",
		Func:  cleanFunc,
		Usage: "",
		Short: "Remove generated UI files and hosts entries",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("devlocal-clean", flag.ExitOnError)
			fs.String("index-dir", "", "Directory containing generated UI files")
			return fs
		}(),
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

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return 1, fmt.Errorf("getting cache dir: %w", err)
	}
	indexDir := fs.String("index-dir")
	if indexDir == "" {
		indexDir = filepath.Join(cacheDir, "caddy-dev-local")
	}

	httpPort, httpsPort, err := loadUserCaddyConfig(configPath)
	if err != nil {
		return 1, fmt.Errorf("loading initial Caddy config: %w", err)
	}
	api := caddyapi.New(caddyapi.Options{
		BaseURL:           fs.String("caddy-admin"),
		ServerName:        fs.String("caddy-server"),
		AllowCreateServer: true,
		HTTPPort:          httpPort,
		HTTPSPort:         httpsPort,
	})
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := controller.Run(ctx, controller.Options{
		Config:      cfg,
		AdminClient: api,
		Logger:      logger,
		IndexDir:    indexDir,
		Caddy:       true,
		UI:          fs.Bool("ui"),
		Hosts:       cfg.HostsFile,
	}); err != nil {
		return 1, err
	}
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

func cleanFunc(fs caddycmd.Flags) (int, error) {
	logger := caddy.Log().Named(appName)
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return 1, fmt.Errorf("getting cache dir: %w", err)
	}
	indexDir := fs.String("index-dir")
	if indexDir == "" {
		indexDir = filepath.Join(cacheDir, "caddy-dev-local")
	}
	if err := controller.Cleanup(context.Background(), controller.Options{
		Config:   config.DefaultConfig(),
		Logger:   logger,
		IndexDir: indexDir,
		UI:       true,
		Hosts:    true,
	}); err != nil {
		return 1, err
	}
	logger.Info("devlocal artifacts removed")
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
