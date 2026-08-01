package caddydevlocal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"go.uber.org/zap"

	"github.com/jsearles/caddy-dev-local/config"
	"github.com/jsearles/caddy-dev-local/generator"
)

func initCaddyConfig(gen *generator.Generator, cfg *config.Config, indexDir string, api *adminAPI, userConfigPath string) error {
	logger := caddy.Log().Named("devlocal")
	indexPage := generator.GenerateIndexPage(cfg.TLD, cfg.Standalone, gen.Containers())
	if err := os.WriteFile(filepath.Join(indexDir, "index.html"), []byte(indexPage), 0600); err != nil {
		return fmt.Errorf("writing index page: %w", err)
	}

	httpPort, httpsPort := 80, 443

	userJSON := []byte("{}")
	if userConfigPath != "" {
		adapterName := adapterFor(userConfigPath)
		userText, err := os.ReadFile(userConfigPath) //nolint:gosec
		if err != nil {
			return fmt.Errorf("reading user config: %w", err)
		}
		if adapterName == adapterCaddyfile {
			parsedHTTP, parsedHTTPS, _, pErr := parseCaddyfileListenPorts(userText)
			if pErr != nil {
				return fmt.Errorf("parsing caddyfile: %w", pErr)
			}
			if parsedHTTP > 0 {
				httpPort = parsedHTTP
			}
			if parsedHTTPS > 0 {
				httpsPort = parsedHTTPS
			}
		}
		userJSON, err = adaptUserConfig(userText, adapterName)
		if err != nil {
			return fmt.Errorf("adapting user config: %w", err)
		}
	}

	userJSON, err := injectListenPorts(userJSON, httpPort, httpsPort)
	if err != nil {
		return fmt.Errorf("injecting listen ports: %w", err)
	}

	devlocal, err := buildDevlocalConfig(cfg.TLD, indexDir, gen.DomainTargets())
	if err != nil {
		return fmt.Errorf("building devlocal config: %w", err)
	}

	if err := caddy.Load(userJSON, false); err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := postDevlocalViaAPI(api, devlocal); err != nil {
		return fmt.Errorf("applying devlocal config: %w", err)
	}

	logger.Info("loaded initial config", zap.Int("domains", len(gen.Domains())))
	return nil
}

func parseCaddyfileListenPorts(source []byte) (httpPort, httpsPort int, ok bool, err error) {
	if len(bytes.TrimSpace(source)) == 0 {
		return 0, 0, false, nil
	}
	blocks, err := caddyfile.Parse("Caddyfile", source)
	if err != nil {
		return 0, 0, false, err
	}
	if len(blocks) == 0 || len(blocks[0].Keys) > 0 {
		return 0, 0, false, nil
	}
	for _, segment := range blocks[0].Segments {
		disp := caddyfile.NewDispenser(segment)
		if !disp.Next() {
			continue
		}
		switch disp.Val() {
		case "http_port":
			port, pErr := parsePortOption(disp, "http_port")
			if pErr != nil {
				return 0, 0, false, pErr
			}
			httpPort = port
		case "https_port":
			port, pErr := parsePortOption(disp, "https_port")
			if pErr != nil {
				return 0, 0, false, pErr
			}
			httpsPort = port
		}
	}
	return httpPort, httpsPort, httpPort > 0 || httpsPort > 0, nil
}

func parsePortOption(disp *caddyfile.Dispenser, name string) (int, error) {
	var str string
	if !disp.AllArgs(&str) {
		return 0, fmt.Errorf("%s requires a single argument at line %d", name, disp.Line())
	}
	port, err := strconv.Atoi(str)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, str, err)
	}
	return port, nil
}

func adaptUserConfig(source []byte, adapterName string) ([]byte, error) {
	if adapterName == adapterJSON {
		return source, nil
	}
	if adapterName == adapterCaddyfile && len(bytes.TrimSpace(source)) == 0 {
		return []byte("{}"), nil
	}
	jsonAdapter := caddyconfig.GetAdapter(adapterName)
	if jsonAdapter == nil {
		return nil, fmt.Errorf("%s adapter not found", adapterName)
	}
	adapted, _, err := jsonAdapter.Adapt(source, nil)
	if err != nil {
		return nil, fmt.Errorf("adapting %s config: %w", adapterName, err)
	}
	return adapted, nil
}

func injectListenPorts(userJSON []byte, httpPort, httpsPort int) ([]byte, error) {
	var cfg map[string]any
	if err := json.Unmarshal(userJSON, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling user config: %w", err)
	}
	apps, _ := cfg["apps"].(map[string]any)
	httpApp, _ := apps["http"].(map[string]any)
	if servers, _ := httpApp["servers"].(map[string]any); len(servers) > 0 {
		return userJSON, nil
	}
	if httpApp == nil {
		httpApp = map[string]any{}
		if apps == nil {
			apps = map[string]any{}
			cfg["apps"] = apps
		}
		apps["http"] = httpApp
	}
	if _, ok := httpApp["http_port"]; !ok {
		httpApp["http_port"] = httpPort
	}
	if _, ok := httpApp["https_port"]; !ok {
		httpApp["https_port"] = httpsPort
	}
	return json.Marshal(cfg)
}

func postDevlocalViaAPI(api *adminAPI, devlocal *devlocalConfig) error {
	if err := api.ensureServer(); err != nil {
		return fmt.Errorf("ensuring server: %w", err)
	}
	if err := api.postRoute(devlocal.indexRoute); err != nil {
		return fmt.Errorf("posting index route: %w", err)
	}
	return api.reconcileDevlocal(devlocal.routes, devlocal.policies)
}

func reloadCaddyConfig(gen *generator.Generator, cfg *config.Config, indexDir string, api *adminAPI) error {
	indexPage := generator.GenerateIndexPage(cfg.TLD, cfg.Standalone, gen.Containers())
	if err := os.WriteFile(filepath.Join(indexDir, "index.html"), []byte(indexPage), 0600); err != nil {
		return fmt.Errorf("writing index page: %w", err)
	}

	devlocal, err := buildDevlocalConfig(cfg.TLD, indexDir, gen.DomainTargets())
	if err != nil {
		return fmt.Errorf("building devlocal config: %w", err)
	}

	return api.reconcileDevlocal(devlocal.routes, devlocal.policies)
}
