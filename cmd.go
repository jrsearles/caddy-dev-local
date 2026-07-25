package caddydevlocal

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/caddyserver/caddy/v2"
	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/jsearles/caddy-dev-local/config"
	"github.com/jsearles/caddy-dev-local/docker"
	"github.com/jsearles/caddy-dev-local/generator"
	"github.com/jsearles/caddy-dev-local/hosts"
	"github.com/moby/moby/client"
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

			fs.Duration("poll-interval", 0,
				"Poll interval (env: DEVLOCAL_POLL_INTERVAL)")

			fs.Duration("probe-timeout", 0,
				"HTTP probe timeout (env: DEVLOCAL_PROBE_TIMEOUT)")

			fs.Bool("hosts-file", true,
				"Manage /etc/hosts entries for domains (env: DEVLOCAL_HOSTS_FILE)")

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
	if v := fs.Duration("poll-interval"); v > 0 {
		cfg.PollInterval = v
	}
	if v := fs.Duration("probe-timeout"); v > 0 {
		cfg.ProbeTimeout = v
	}

	hostsFile := fs.Bool("hosts-file")
	cfg.ApplyFlags(
		fs.String("ingress-network"),
		fs.String("tld"),
		fs.Duration("stale-ttl"),
		fs.Duration("poll-interval"),
		fs.Duration("probe-timeout"),
		&hostsFile,
	)

	ingressExplicit := fs.String("ingress-network") != "" || os.Getenv("DEVLOCAL_INGRESS_NETWORK") != ""
	if !ingressExplicit {
		cfg.Standalone = config.DetectStandalone()
	}

	hostsOK := true
	if cfg.HostsFile {
		if !hosts.CanWrite() {
			log.Println("[devlocal] warning: hosts file not writable, skipping hosts file updates")
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

	if err := gen.Refresh(ctx); err != nil {
		log.Printf("[devlocal] initial refresh failed: %v", err)
	}

	if err := gen.SelectPorts(ctx); err != nil {
		log.Printf("[devlocal] initial port selection failed: %v", err)
	}

	if err := loadCaddyConfig(gen, cfg); err != nil {
		return 1, fmt.Errorf("loading initial caddy config: %w", err)
	}

	syncHosts(gen, cfg, hostsOK)

	go watchEvents(ctx, dockerClient, gen, cfg, hostsOK)
	go pollContainers(ctx, cfg, gen, hostsOK)
	go staleCleanup(ctx, cfg, gen, hostsOK)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	cancel()
	return 0, nil
}

func watchEvents(ctx context.Context, dockerClient docker.Client, gen *generator.Generator, cfg *config.Config, hostsOK bool) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		f := client.Filters{}
		f.Add("type", "container")

		msgCh, errCh := dockerClient.Events(ctx, client.EventsListOptions{
			Filters: f,
		})

		throttle := time.NewTimer(100 * time.Millisecond)
		pending := false

	eventLoop:
		for {
			select {
			case <-ctx.Done():
				throttle.Stop()
				return
			case event, ok := <-msgCh:
				if !ok {
					break eventLoop
				}
				switch event.Action {
				case "start", "stop", "die", "destroy", "create":
					if !pending {
						pending = true
						throttle.Reset(100 * time.Millisecond)
					}
				}
			case err, ok := <-errCh:
				if !ok || err != nil {
					log.Printf("[devlocal] event stream error: %v, reconnecting in 30s", err)
					time.Sleep(30 * time.Second)
					break eventLoop
				}
			case <-throttle.C:
				if pending {
					pending = false
					gen.Refresh(ctx)
					gen.SelectPorts(ctx)
					loadCaddyConfig(gen, cfg)
					syncHosts(gen, cfg, hostsOK)
				}
				throttle.Stop()
				break eventLoop
			}
		}
	}
}

func pollContainers(ctx context.Context, cfg *config.Config, gen *generator.Generator, hostsOK bool) {
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			gen.Refresh(ctx)
			gen.SelectPorts(ctx)
			loadCaddyConfig(gen, cfg)
			syncHosts(gen, cfg, hostsOK)
		}
	}
}

func staleCleanup(ctx context.Context, cfg *config.Config, gen *generator.Generator, hostsOK bool) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			gen.StaleCleanup()
			loadCaddyConfig(gen, cfg)
			syncHosts(gen, cfg, hostsOK)
		}
	}
}

func loadCaddyConfig(gen *generator.Generator, cfg *config.Config) error {
	caddyfile := gen.GenerateCaddyfile()

	indexPage := generator.GenerateIndexPage(cfg.TLD, cfg.Standalone, gen.Containers())
	indexBlock := fmt.Sprintf("%s {\n    tls internal\n    respond `%s` 200\n}\n",
		cfg.TLD, strings.ReplaceAll(indexPage, "`", "\\`"))

	fullCaddyfile := indexBlock + "\n" + caddyfile

	adapter := caddyconfig.GetAdapter("caddyfile")
	if adapter == nil {
		return fmt.Errorf("caddyfile adapter not found")
	}

	configJSON, _, err := adapter.Adapt([]byte(fullCaddyfile), nil)
	if err != nil {
		return fmt.Errorf("adapting caddyfile: %w", err)
	}

	return caddy.Load(configJSON, false)
}

func syncHosts(gen *generator.Generator, cfg *config.Config, hostsOK bool) {
	if !cfg.HostsFile || !hostsOK {
		return
	}
	if err := hosts.Sync(gen.Domains()); err != nil {
		log.Printf("[devlocal] failed to update hosts file: %v", err)
	}
}

func cleanFunc(fs caddycmd.Flags) (int, error) {
	if err := hosts.Remove(); err != nil {
		log.Printf("[devlocal] failed to remove hosts entries: %v", err)
		return 1, err
	}
	log.Println("[devlocal] hosts file entries removed")
	return 0, nil
}
