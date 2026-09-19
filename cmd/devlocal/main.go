package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/pflag"

	"github.com/jrsearles/caddy-dev-local/caddyapi"
	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/controller"
)

const name = "devlocal"

func main() {
	clean := len(os.Args) > 1 && os.Args[1] == "clean"
	args := os.Args[1:]
	if clean {
		args = args[1:]
	}
	if err := run(args, clean); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(1)
	}
}

func run(args []string, clean bool) error {
	cfg := config.DefaultConfig()
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("getting cache directory: %w", err)
	}

	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [clean] [flags]\n\nFlags:\n", name)
		fs.PrintDefaults()
	}
	config.RegisterSharedFlags(fs)
	caddyEnabled := fs.Bool("caddy", true, "Register routes with Caddy")
	uiEnabled := fs.Bool("ui", true, "Generate and register the index UI")
	hostsEnabled := fs.Bool("hosts-file", cfg.HostsFile, "Manage the hosts file")
	adminURL := fs.String("caddy-admin", envOrDefault("DEVLOCAL_CADDY_ADMIN", "http://localhost:2019"), "Caddy admin API URL")
	serverName := fs.String("caddy-server", envOrDefault("DEVLOCAL_CADDY_SERVER", "srv0"), "Caddy HTTP server name")
	allowCreate := fs.Bool("allow-create-server", true, "Create the target Caddy HTTP server when absent")
	indexDir := fs.String("index-dir", envOrDefault("DEVLOCAL_INDEX_DIR", filepath.Join(cacheDir, "caddy-dev-local")), "Directory for generated UI files")
	logLevel := fs.String("log-level", envOrDefault("DEVLOCAL_LOG_LEVEL", "info"), "Log level: debug, info, warn, or error")
	noTracing := fs.Bool("no-tracing", !cfg.Tracing, "Disable OpenTelemetry tracing on dynamic routes")
	if parseErr := fs.Parse(args); parseErr != nil {
		if errors.Is(parseErr, pflag.ErrHelp) {
			return nil
		}
		return parseErr
	}
	config.ApplySharedFlags(cfg, fs)
	cfg.HostsFile = *hostsEnabled
	cfg.Tracing = !*noTracing

	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		return fmt.Errorf("invalid log level %q: %w", *logLevel, err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	api := caddyapi.New(caddyapi.Options{
		BaseURL:           *adminURL,
		ServerName:        *serverName,
		AllowCreateServer: *allowCreate,
	})
	options := controller.Options{
		Config:      cfg,
		AdminClient: api,
		Logger:      logger.With("service", name),
		IndexDir:    *indexDir,
		Caddy:       *caddyEnabled,
		UI:          *uiEnabled,
		Hosts:       *hostsEnabled,
	}
	if clean {
		return controller.Cleanup(ctx, options)
	}
	return controller.Run(ctx, options)
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
