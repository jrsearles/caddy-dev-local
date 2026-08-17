package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/spf13/pflag"
	"go.uber.org/zap"

	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
	"github.com/jrsearles/caddy-dev-local/docker"
	"github.com/jrsearles/caddy-dev-local/generator"
	"github.com/jrsearles/caddy-dev-local/hosts"
)

const name = "devlocal-hosts"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "clean" {
		if err := hosts.Remove(); err != nil {
			fmt.Fprintf(os.Stderr, "%s: removing hosts entries: %v\n", name, err)
			os.Exit(1)
		}
		return
	}
	run()
}

func run() {
	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n\nFlags:\n", name)
		fs.PrintDefaults()
	}
	config.RegisterSharedFlags(fs)
	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		os.Exit(1)
	}

	cfg := config.Resolve(fs)
	cfg.HostsFile = true

	logger := zap.NewExample()

	if !hosts.CanWrite() {
		fmt.Fprintln(os.Stderr, "hosts file not writable, exiting (run as root)")
		os.Exit(1)
	}

	dockerClient, err := docker.NewClient()
	if err != nil {
		logger.Error("creating docker client", zap.Error(err))
		os.Exit(1)
	}

	gen := generator.NewGenerator(cfg, dockerClient)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := gen.Refresh(ctx); err != nil {
		logger.Error("initial refresh failed", zap.Error(err))
	}

	var applyMu sync.Mutex
	apply := func() {
		applyMu.Lock()
		defer applyMu.Unlock()
		if err := hosts.Sync(cfg.TLD, gen.Domains()); err != nil {
			logger.Error("failed to update hosts file", zap.Error(err))
		}
	}

	apply()
	logger.Info("discovered containers", zap.Int("count", len(gen.Containers())))

	discovery.New(cfg, dockerClient, gen, gen.Refresh, apply, logger).Run(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	cancel()
}
