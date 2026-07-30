package caddydevlocal

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
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

func initCaddyConfig(gen *generator.Generator, cfg *config.Config, indexDir string, api *adminAPI, userConfigPath string) error {
	logger := caddy.Log().Named("devlocal")
	indexPage := generator.GenerateIndexPage(cfg.TLD, cfg.Standalone, gen.Containers())
	if err := os.WriteFile(filepath.Join(indexDir, "index.html"), []byte(indexPage), 0600); err != nil {
		return fmt.Errorf("writing index page: %w", err)
	}

	indexBlock := fmt.Sprintf("%s {\n    root * %s\n    file_server\n}\n",
		cfg.TLD, indexDir)

	devlocalCaddyfile := gen.GenerateCaddyfile()

	if userConfigPath != "" {
		adapterName := adapterFor(userConfigPath)
		if adapterName == "caddyfile" {
			userText, err := os.ReadFile(userConfigPath) //nolint:gosec
			if err != nil {
				return fmt.Errorf("reading user config: %w", err)
			}
			fullText := string(userText) + "\n" + indexBlock + "\n" + devlocalCaddyfile
			return loadCaddyfileConfig([]byte(fullText), cfg.TLD, api)
		}
		return loadStructuredConfig(api, userConfigPath, adapterName, gen, cfg, indexBlock)
	}

	fullText := indexBlock + "\n" + devlocalCaddyfile
	if err := loadCaddyfileConfig([]byte(fullText), cfg.TLD, api); err != nil {
		return err
	}
	logger.Info("loaded initial config", zap.Int("domains", len(gen.Domains())))
	return nil
}

func loadCaddyfileConfig(source []byte, tld string, api *adminAPI) error {
	adapter := caddyconfig.GetAdapter("caddyfile")
	if adapter == nil {
		return fmt.Errorf("caddyfile adapter not found")
	}
	configJSON, _, err := adapter.Adapt(source, nil)
	if err != nil {
		return fmt.Errorf("adapting caddyfile: %w", err)
	}

	modifiedJSON, routeIDs, policyIDs, err := injectDevlocalIDs(configJSON, tld)
	if err != nil {
		return fmt.Errorf("injecting devlocal IDs: %w", err)
	}

	if err := caddy.Load(modifiedJSON, false); err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	api.prevRouteIDs = routeIDs
	api.prevPolicyIDs = policyIDs
	return nil
}

func loadStructuredConfig(api *adminAPI, configPath, adapterName string, gen *generator.Generator, cfg *config.Config, indexBlock string) error {
	userRaw, err := os.ReadFile(configPath) //nolint:gosec
	if err != nil {
		return fmt.Errorf("reading user config: %w", err)
	}

	var userConfig []byte
	if adapterName == "json" {
		userConfig = userRaw
	} else {
		jsonAdapter := caddyconfig.GetAdapter(adapterName)
		if jsonAdapter == nil {
			return fmt.Errorf("%s adapter not found", adapterName)
		}
		adapted, _, aErr := jsonAdapter.Adapt(userRaw, nil)
		if aErr != nil {
			return fmt.Errorf("adapting user config: %w", aErr)
		}
		userConfig = adapted
	}

	if err = api.loadConfig(userConfig, "application/json"); err != nil {
		return fmt.Errorf("loading user config via admin API: %w", err)
	}

	if err = api.ensureServer(); err != nil {
		return fmt.Errorf("ensuring server: %w", err)
	}

	devlocalCaddyfile := indexBlock + "\n" + gen.GenerateCaddyfile()

	adapter := caddyconfig.GetAdapter("caddyfile")
	if adapter == nil {
		return fmt.Errorf("caddyfile adapter not found")
	}
	configJSON, _, err := adapter.Adapt([]byte(devlocalCaddyfile), nil)
	if err != nil {
		return fmt.Errorf("adapting devlocal config: %w", err)
	}

	modifiedJSON, _, _, err := injectDevlocalIDs(configJSON, cfg.TLD)
	if err != nil {
		return fmt.Errorf("injecting devlocal IDs: %w", err)
	}

	return api.syncDevlocal(modifiedJSON)
}

func reloadCaddyConfig(gen *generator.Generator, cfg *config.Config, indexDir string, api *adminAPI) error {
	indexPage := generator.GenerateIndexPage(cfg.TLD, cfg.Standalone, gen.Containers())
	if err := os.WriteFile(filepath.Join(indexDir, "index.html"), []byte(indexPage), 0600); err != nil {
		return fmt.Errorf("writing index page: %w", err)
	}

	devlocalCaddyfile := gen.GenerateCaddyfile()
	if devlocalCaddyfile == "" {
		api.clearDevlocal()
		return nil
	}

	adapter := caddyconfig.GetAdapter("caddyfile")
	if adapter == nil {
		return fmt.Errorf("caddyfile adapter not found")
	}
	configJSON, _, err := adapter.Adapt([]byte(devlocalCaddyfile), nil)
	if err != nil {
		return fmt.Errorf("adapting devlocal config: %w", err)
	}

	modifiedJSON, _, _, err := injectDevlocalIDs(configJSON, cfg.TLD)
	if err != nil {
		return fmt.Errorf("injecting devlocal IDs: %w", err)
	}

	if err := api.ensureServer(); err != nil {
		return fmt.Errorf("ensuring server: %w", err)
	}

	return api.syncDevlocal(modifiedJSON)
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

func adapterFor(configPath string) string {
	switch strings.ToLower(filepath.Ext(configPath)) {
	case ".json":
		return "json"
	case ".json5":
		return "json5"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return "caddyfile"
	}
}

func injectDevlocalIDs(rawJSON []byte, tld string) ([]byte, []string, []string, error) {
	var config map[string]interface{}
	if err := json.Unmarshal(rawJSON, &config); err != nil {
		return nil, nil, nil, err
	}

	var routeIDs []string
	var policyIDs []string

	apps, _ := config["apps"].(map[string]interface{})

	if http, ok := apps["http"].(map[string]interface{}); ok {
		if servers, ok := http["servers"].(map[string]interface{}); ok {
			for _, srv := range servers {
				srvMap, _ := srv.(map[string]interface{})
				if routes, ok := srvMap["routes"].([]interface{}); ok {
					for _, r := range routes {
						route, _ := r.(map[string]interface{})
						if route != nil {
							if host, ok := extractDevlocalHost(route, tld); ok {
								id := "devlocal-route-" + strings.ReplaceAll(host, ".", "-")
								route["@id"] = id
								routeIDs = append(routeIDs, id)
							}
						}
					}
				}
			}
		}
	}

	if tls, ok := apps["tls"].(map[string]interface{}); ok {
		if automation, ok := tls["automation"].(map[string]interface{}); ok {
			if policies, ok := automation["policies"].([]interface{}); ok {
				for _, p := range policies {
					policy, _ := p.(map[string]interface{})
					if policy != nil {
						if subject, ok := extractDevlocalSubject(policy, tld); ok {
							id := "devlocal-tls-" + strings.ReplaceAll(subject, ".", "-")
							policy["@id"] = id
							policyIDs = append(policyIDs, id)
						}
					}
				}
			}
		}
	}

	result, err := json.Marshal(config)
	return result, routeIDs, policyIDs, err
}

func extractDevlocalHost(route map[string]interface{}, tld string) (string, bool) {
	match, _ := route["match"].([]interface{})
	if len(match) == 0 {
		return "", false
	}
	firstMatch, _ := match[0].(map[string]interface{})
	if firstMatch == nil {
		return "", false
	}
	hosts, _ := firstMatch["host"].([]interface{})
	if len(hosts) == 0 {
		return "", false
	}
	host, _ := hosts[0].(string)
	if host == "" {
		return "", false
	}
	if isDevlocalDomain(host, tld) {
		return host, true
	}
	return "", false
}

func extractDevlocalSubject(policy map[string]interface{}, tld string) (string, bool) {
	subjects, _ := policy["subjects"].([]interface{})
	if len(subjects) == 0 {
		return "", false
	}
	subject, _ := subjects[0].(string)
	if subject == "" {
		return "", false
	}
	if isDevlocalDomain(subject, tld) {
		return subject, true
	}
	return "", false
}

func isDevlocalDomain(host, tld string) bool {
	if strings.HasSuffix(host, "."+tld) {
		return true
	}
	if strings.HasSuffix(host, ".localhost") {
		return true
	}
	return false
}
