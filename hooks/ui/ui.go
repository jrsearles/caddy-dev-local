package ui

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
	"github.com/jrsearles/caddy-dev-local/generator"
)

const (
	indexHTMLName = "index.html"
	indexCSSName  = "index.css"
	versionName   = "version.json"

	indexRouteID = "devlocal-index"
	tlsPolicyID  = "devlocal-tls-ui"

	keyID      = "@id"
	keyHandle  = "handle"
	keyHandler = "handler"
)

// caddyClient is the subset of caddyapi.Client the UI hook needs to serve the
// generated index page through Caddy.
type caddyClient interface {
	RunningConfig(context.Context) (string, error)
	Reconcile(context.Context, map[string]json.RawMessage, map[string]json.RawMessage, func(string) bool) error
	Cleanup(context.Context) error
}

type Hook struct {
	cfg       *config.Config
	outputDir string
	caddy     caddyClient

	mu           sync.Mutex
	cachedConfig string
}

func New(cfg *config.Config, outputDir string, caddy caddyClient) *Hook {
	return &Hook{cfg: cfg, outputDir: outputDir, caddy: caddy}
}

func (p *Hook) Name() string {
	return "ui"
}

func (p *Hook) Apply(ctx context.Context, update discovery.Update) error { //nolint:gocritic
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.outputDir == "" {
		return fmt.Errorf("ui hook: output directory is empty")
	}
	if p.caddy != nil {
		if runningConfig, err := p.caddy.RunningConfig(ctx); err == nil {
			p.cachedConfig = runningConfig
		}
	}

	snapshot := canonicalSnapshot(update.Snapshot)
	lastRefresh := int64(0)
	if !update.Status.LastRefresh.IsZero() {
		lastRefresh = update.Status.LastRefresh.Unix()
	}
	page := generator.GenerateIndexPage(p.cfg.TLD, snapshot, p.cachedConfig, update.Status.LastError, lastRefresh)
	fingerprint := fingerprint(page, generator.IndexCSS)

	if err := os.MkdirAll(p.outputDir, 0755); err != nil {
		return fmt.Errorf("creating UI output directory: %w", err)
	}
	if err := os.Chmod(p.outputDir, 0755); err != nil {
		return fmt.Errorf("setting UI output directory permissions: %w", err)
	}
	if err := writeIfChanged(filepath.Join(p.outputDir, indexHTMLName), []byte(page)); err != nil {
		return err
	}
	if err := writeIfChanged(filepath.Join(p.outputDir, indexCSSName), []byte(generator.IndexCSS)); err != nil {
		return err
	}
	version, err := json.Marshal(struct {
		Version string `json:"v"`
	}{Version: fingerprint})
	if err != nil {
		return fmt.Errorf("encoding UI version: %w", err)
	}
	if err := writeIfChanged(filepath.Join(p.outputDir, versionName), version); err != nil {
		return err
	}

	if p.caddy != nil {
		reserved := make(map[string]bool, len(update.Snapshot))
		for domain := range generator.DomainTargets(p.cfg, update.Snapshot) {
			reserved[domain] = true
		}
		routes, policies, err := p.indexResources(reserved)
		if err != nil {
			return err
		}
		if err := p.caddy.Reconcile(ctx, routes, policies, p.owned); err != nil {
			return fmt.Errorf("reconciling index UI in Caddy: %w", err)
		}
	}
	return nil
}

func (p *Hook) Cleanup(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.outputDir == "" {
		return fmt.Errorf("ui hook: output directory is empty")
	}

	var cleanupErrors []error
	for _, name := range []string{indexHTMLName, indexCSSName, versionName} {
		if err := os.Remove(filepath.Join(p.outputDir, name)); err != nil && !os.IsNotExist(err) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("removing %s: %w", name, err))
		}
	}
	if p.caddy != nil {
		if err := p.caddy.Cleanup(ctx); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("cleaning Caddy resources: %w", err))
		}
	}
	return errors.Join(cleanupErrors...)
}

// indexResources builds the file-server route and internal-issuer TLS policy
// that serve the generated index page at the TLD and its .localhost alias,
// skipping hosts already claimed by container routes.
func (p *Hook) indexResources(reserved map[string]bool) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	indexHosts := make([]string, 0, 2)
	for _, host := range []string{p.cfg.TLD, generator.TLDLocalhost(p.cfg.TLD)} {
		if !reserved[host] && !slices.Contains(indexHosts, host) {
			indexHosts = append(indexHosts, host)
		}
	}
	if len(indexHosts) == 0 {
		return map[string]json.RawMessage{}, map[string]json.RawMessage{}, nil
	}
	indexRoute, err := json.Marshal(map[string]any{
		keyID: indexRouteID,
		keyHandle: []any{map[string]any{
			keyHandler: "subroute",
			"routes": []any{map[string]any{keyHandle: []any{
				map[string]any{keyHandler: "vars", "root": p.outputDir},
				map[string]any{keyHandler: "file_server", "hide": []string{"./Caddyfile"}},
			}}},
		}},
		"match":    []any{map[string]any{"host": indexHosts}},
		"terminal": true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("building index route: %w", err)
	}
	policy, err := json.Marshal(map[string]any{
		keyID: tlsPolicyID, "issuers": []any{map[string]any{"module": "internal"}}, "subjects": indexHosts,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("building index TLS policy: %w", err)
	}
	return map[string]json.RawMessage{indexRouteID: indexRoute}, map[string]json.RawMessage{tlsPolicyID: policy}, nil
}

// owned reports identifiers this hook manages: the index route and its TLS
// policy. Container routes and policies are left to the caddy hook.
func (p *Hook) owned(id string) bool {
	return id == indexRouteID || id == tlsPolicyID
}

func canonicalSnapshot(snapshot []*discovery.ContainerInfo) []*discovery.ContainerInfo {
	result := make([]*discovery.ContainerInfo, 0, len(snapshot))
	for _, info := range snapshot {
		if info == nil {
			continue
		}
		clone := *info
		clone.Ports = slices.Clone(info.Ports)
		slices.Sort(clone.Ports)
		clone.Networks = slices.Clone(info.Networks)
		slices.Sort(clone.Networks)
		clone.CustomDomains = slices.Clone(info.CustomDomains)
		slices.SortFunc(clone.CustomDomains, func(a, b discovery.CustomDomain) int {
			if n := cmp.Compare(a.Domain, b.Domain); n != 0 {
				return n
			}
			return cmp.Compare(a.Port, b.Port)
		})
		result = append(result, &clone)
	}
	slices.SortFunc(result, func(a, b *discovery.ContainerInfo) int {
		return cmp.Compare(a.ContainerID, b.ContainerID)
	})
	return result
}

func fingerprint(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeIfChanged(path string, content []byte) error {
	if existing, err := os.ReadFile(path); err == nil && slices.Equal(existing, content) {
		if err := os.Chmod(path, 0644); err != nil {
			return fmt.Errorf("setting permissions on %s: %w", filepath.Base(path), err)
		}
		return nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".devlocal-*")
	if err != nil {
		return fmt.Errorf("creating temporary %s: %w", filepath.Base(path), err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return fmt.Errorf("setting permissions on %s: %w", filepath.Base(path), err)
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publishing %s: %w", filepath.Base(path), err)
	}
	return nil
}
