package caddydevlocal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func loadUserCaddyConfig(userConfigPath string) (int, int, error) {
	httpPort, httpsPort := 80, 443
	userJSON := []byte("{}")
	if userConfigPath != "" {
		adapterName := adapterFor(userConfigPath)
		userText, err := os.ReadFile(userConfigPath) //nolint:gosec
		if err != nil {
			return 0, 0, fmt.Errorf("reading user config: %w", err)
		}
		if adapterName == adapterCaddyfile {
			parsedHTTP, parsedHTTPS, _, parseErr := parseCaddyfileListenPorts(userText)
			if parseErr != nil {
				return 0, 0, fmt.Errorf("parsing Caddyfile: %w", parseErr)
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
			return 0, 0, err
		}
	}
	var err error
	userJSON, err = injectListenPorts(userJSON, httpPort, httpsPort)
	if err != nil {
		return 0, 0, fmt.Errorf("injecting listen ports: %w", err)
	}
	if err := caddy.Load(userJSON, false); err != nil {
		return 0, 0, fmt.Errorf("loading config: %w", err)
	}
	return httpPort, httpsPort, nil
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
			port, parseErr := parsePortOption(disp, "http_port")
			if parseErr != nil {
				return 0, 0, false, parseErr
			}
			httpPort = port
		case "https_port":
			port, parseErr := parsePortOption(disp, "https_port")
			if parseErr != nil {
				return 0, 0, false, parseErr
			}
			httpsPort = port
		}
	}
	return httpPort, httpsPort, httpPort > 0 || httpsPort > 0, nil
}

func parsePortOption(disp *caddyfile.Dispenser, name string) (int, error) {
	var value string
	if !disp.AllArgs(&value) {
		return 0, fmt.Errorf("%s requires a single argument at line %d", name, disp.Line())
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, value, err)
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
