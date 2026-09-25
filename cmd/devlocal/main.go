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

const (
	name             = "devlocal"
	startCommandName = "start"
	cleanCommandName = "clean"
)

type command int

const (
	commandRunOnce command = iota
	commandStart
	commandClean
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd, args, err := parseCommand(args)
	if err != nil {
		return err
	}
	cfg := config.DefaultConfig()
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("getting cache directory: %w", err)
	}

	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [%s|%s] [flags]\n\nFlags:\n", name, startCommandName, cleanCommandName)
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
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
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
	switch cmd {
	case commandStart:
		return controller.Run(ctx, options)
	case commandClean:
		return controller.Cleanup(ctx, options)
	default:
		return controller.RunOnce(ctx, options)
	}
}

func parseCommand(args []string) (command, []string, error) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		return commandRunOnce, args, nil
	}
	switch args[0] {
	case startCommandName:
		return commandStart, args[1:], nil
	case cleanCommandName:
		return commandClean, args[1:], nil
	default:
		return commandRunOnce, nil, fmt.Errorf("unknown command %q", args[0])
	}
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
