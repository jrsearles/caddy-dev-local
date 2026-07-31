package caddydevlocal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
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
			warn, err := loadUserGlobalOptions(userText)
			if err != nil {
				return fmt.Errorf("loading user global options: %w", err)
			}
			if warn != "" {
				logger.Warn(warn)
			}
			devlocalText := indexBlock + "\n" + devlocalCaddyfile
			return loadDevlocalViaAPI([]byte(devlocalText), cfg.TLD, api)
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

func loadUserGlobalOptions(source []byte) (string, error) {
	adapter := caddyconfig.GetAdapter("caddyfile")
	if adapter == nil {
		return "", fmt.Errorf("caddyfile adapter not found")
	}
	configJSON, _, err := adapter.Adapt(source, nil)
	if err != nil {
		return "", fmt.Errorf("adapting caddyfile: %w", err)
	}

	var config map[string]any
	if uErr := json.Unmarshal(configJSON, &config); uErr != nil {
		return "", uErr
	}

	var warn string
	if apps, ok := config["apps"].(map[string]any); ok {
		if http, ok := apps["http"].(map[string]any); ok {
			if servers, ok := http["servers"].(map[string]any); ok {
				for name := range servers {
					warn = fmt.Sprintf("static Caddyfile contains site block for server %q; site blocks are ignored. Use the admin API for site configuration.", name)
					break
				}
			}
		}
	}

	delete(config, "apps")

	if len(config) > 0 {
		result, err := json.Marshal(config)
		if err != nil {
			return "", err
		}
		if err := caddy.Load(result, false); err != nil {
			return "", fmt.Errorf("loading global options: %w", err)
		}
	}

	return warn, nil
}

func loadDevlocalViaAPI(source []byte, tld string, api *adminAPI) error {
	if len(source) == 0 {
		return nil
	}
	adapter := caddyconfig.GetAdapter("caddyfile")
	if adapter == nil {
		return fmt.Errorf("caddyfile adapter not found")
	}
	configJSON, _, err := adapter.Adapt(source, nil)
	if err != nil {
		return fmt.Errorf("adapting devlocal config: %w", err)
	}

	modifiedJSON, _, _, err := injectDevlocalIDs(configJSON, tld)
	if err != nil {
		return fmt.Errorf("injecting devlocal IDs: %w", err)
	}

	if err := api.ensureServer(); err != nil {
		return fmt.Errorf("ensuring server: %w", err)
	}

	return api.syncDevlocal(modifiedJSON)
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
