package caddydevlocal

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"go.uber.org/zap"

	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
	"github.com/jrsearles/caddy-dev-local/generator"
)

func initCaddyConfig(gen *generator.Generator, cfg *config.Config, indexDir string, api *adminAPI, userConfigPath string, statusFn func() discovery.Status) error {
	logger := caddy.Log().Named(appName)

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

	devlocal, err := buildDevlocalConfig(cfg.TLD, indexDir, gen.DomainTargets(), cfg.Tracing)
	if err != nil {
		return fmt.Errorf("building devlocal config: %w", err)
	}

	if err := caddy.Load(userJSON, false); err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := postDevlocalViaAPI(api, devlocal); err != nil {
		return fmt.Errorf("applying devlocal config: %w", err)
	}

	st := statusFn()
	fp := fingerprintState(gen.DomainTargets(), gen.Containers(), st.LastError)
	indexPage := generator.GenerateIndexPage(cfg.TLD, cfg.Standalone, gen.Containers(), fetchRunningConfig(api), st.LastError, unixOrZero(st.LastRefresh))
	if err := writeIndexArtifacts(indexDir, indexPage, fp); err != nil {
		return fmt.Errorf("writing index page: %w", err)
	}

	api.setFingerprint(fp)
	writeDevlocalAutosave(indexDir, devlocal)

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

func reloadCaddyConfig(gen *generator.Generator, cfg *config.Config, indexDir string, api *adminAPI, statusFn func() discovery.Status) (bool, error) {
	logger := caddy.Log().Named(appName)

	domains := gen.DomainTargets()
	st := statusFn()
	fp := fingerprintState(domains, gen.Containers(), st.LastError)
	if fp == api.fingerprint() {
		logger.Debug("no changes to apply")
		return false, nil
	}

	devlocal, err := buildDevlocalConfig(cfg.TLD, indexDir, domains, cfg.Tracing)
	if err != nil {
		return false, fmt.Errorf("building devlocal config: %w", err)
	}

	if err := api.reconcileDevlocal(devlocal.routes, devlocal.policies); err != nil {
		indexPage := generator.GenerateIndexPage(cfg.TLD, cfg.Standalone, gen.Containers(), fetchRunningConfig(api), st.LastError, unixOrZero(st.LastRefresh))
		if werr := writeIndexArtifacts(indexDir, indexPage, fp); werr != nil {
			return false, fmt.Errorf("writing index page: %w", werr)
		}
		return false, err
	}

	indexPage := generator.GenerateIndexPage(cfg.TLD, cfg.Standalone, gen.Containers(), fetchRunningConfig(api), st.LastError, unixOrZero(st.LastRefresh))
	if err := writeIndexArtifacts(indexDir, indexPage, fp); err != nil {
		return false, fmt.Errorf("writing index page: %w", err)
	}

	api.setFingerprint(fp)
	writeDevlocalAutosave(indexDir, devlocal)
	return true, nil
}

func fingerprintState(domains map[string][]string, containers []*generator.ContainerInfo, discoveryError string) string {
	sorted := slices.Clone(containers)
	slices.SortFunc(sorted, func(a, b *generator.ContainerInfo) int {
		return cmp.Compare(a.ContainerID, b.ContainerID)
	})

	containerState, err := json.Marshal(sorted)
	if err != nil {
		return fingerprintDomains(domains) + "\x00" + discoveryError
	}

	h := sha256.New()
	io.WriteString(h, fingerprintDomains(domains)) //nolint:errcheck // sha256 writer never errors
	h.Write([]byte{0})
	h.Write(containerState)
	io.WriteString(h, discoveryError) //nolint:errcheck // sha256 writer never errors
	return hex.EncodeToString(h.Sum(nil))
}

func fingerprintDomains(targets map[string][]string) string {
	domains := make([]string, 0, len(targets))
	for d := range targets {
		domains = append(domains, d)
	}
	slices.Sort(domains)

	h := sha256.New()
	for _, d := range domains {
		io.WriteString(h, d) //nolint:errcheck // sha256 writer never errors
		h.Write([]byte{0})
		for _, t := range slices.Sorted(slices.Values(targets[d])) {
			io.WriteString(h, t) //nolint:errcheck // sha256 writer never errors
			h.Write([]byte{0})
		}
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeDevlocalAutosave(indexDir string, devlocal *devlocalConfig) {
	logger := caddy.Log().Named(appName)
	data, err := json.MarshalIndent(devlocalAutosave{
		Routes:     devlocal.routes,
		Policies:   devlocal.policies,
		IndexRoute: devlocal.indexRoute,
	}, "", "  ")
	if err != nil {
		logger.Warn("failed to marshal devlocal autosave", zap.Error(err))
		return
	}
	if err := os.WriteFile(filepath.Join(indexDir, "devlocal.json"), data, 0600); err != nil {
		logger.Warn("failed to write devlocal autosave", zap.Error(err))
	}
}

var lastConfigCache atomic.Pointer[string]

func fetchRunningConfig(api *adminAPI) string {
	status, body, err := api.getConfig("/config/")
	if err != nil || status != http.StatusOK {
		caddy.Log().Named(appName).Warn("failed to fetch running config",
			zap.Int("status", status), zap.Error(err))
		if prev := lastConfigCache.Load(); prev != nil {
			return *prev
		}
		return ""
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		return string(body)
	}
	s := pretty.String()
	lastConfigCache.Store(&s)
	return s
}

func writeIndexArtifacts(indexDir, page, fingerprint string) error {
	htmlPath := filepath.Join(indexDir, "index.html")
	if existing, err := os.ReadFile(htmlPath); err != nil || string(existing) != page {
		if err := os.WriteFile(htmlPath, []byte(page), 0600); err != nil {
			return err
		}
	}

	cssPath := filepath.Join(indexDir, "index.css")
	if existing, err := os.ReadFile(cssPath); err != nil || string(existing) != generator.IndexCSS {
		if err := os.WriteFile(cssPath, []byte(generator.IndexCSS), 0600); err != nil {
			return err
		}
	}

	type versionDoc struct {
		V  string `json:"v"`
		Ts int64  `json:"ts"`
	}
	vb, err := json.Marshal(versionDoc{V: fingerprint, Ts: time.Now().Unix()})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(indexDir, "version.json"), vb, 0600)
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}
