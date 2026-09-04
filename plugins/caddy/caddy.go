package caddy

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/jrsearles/caddy-dev-local/caddyapi"
	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
	"github.com/jrsearles/caddy-dev-local/generator"
)

const (
	indexRouteID = "devlocal-index"
	tlsPolicyID  = "devlocal-tls"
	keyID        = "@id"
	keyHandle    = "handle"
	keyHandler   = "handler"
)

type Plugin struct {
	cfg      *config.Config
	indexDir string
	client   *caddyapi.Client
}

func New(cfg *config.Config, indexDir string, client *caddyapi.Client) *Plugin {
	return &Plugin{cfg: cfg, indexDir: indexDir, client: client}
}

func (p *Plugin) Name() string {
	return "caddy"
}

func (p *Plugin) Apply(ctx context.Context, delta discovery.Delta) error { //nolint:gocritic
	if p.cfg == nil {
		return fmt.Errorf("caddy plugin: config is nil")
	}
	if p.client == nil {
		return fmt.Errorf("caddy plugin: API client is nil")
	}
	if delta.Status.LastRefresh.IsZero() && delta.Status.LastError != "" {
		return nil
	}
	targets := generator.DomainTargets(p.cfg, delta.Snapshot)
	routes, policies, err := buildConfig(p.cfg, p.indexDir, targets)
	if err != nil {
		return err
	}
	return p.client.Reconcile(ctx, routes, policies)
}

func (p *Plugin) Cleanup(ctx context.Context) error {
	if p.client == nil {
		return fmt.Errorf("caddy plugin: API client is nil")
	}
	return p.client.Cleanup(ctx)
}

func buildConfig(cfg *config.Config, indexDir string, targets map[string][]string) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	domains := make([]string, 0, len(targets))
	for domain := range targets {
		domains = append(domains, domain)
	}
	slices.Sort(domains)

	routes := make(map[string]json.RawMessage, len(domains)+1)
	for _, domain := range domains {
		id := routeID(domain)
		upstreams := make([]any, 0, len(targets[domain]))
		for _, target := range targets[domain] {
			upstreams = append(upstreams, map[string]any{"dial": target})
		}
		proxy := map[string]any{keyHandler: "reverse_proxy", "upstreams": upstreams}
		handle := []any{}
		if cfg.Tracing {
			handle = append(handle, map[string]any{keyHandler: "tracing", "span": "{http.request.method} {http.request.host}"})
		}
		handle = append(handle, map[string]any{
			keyHandler: "subroute",
			"routes":   []any{map[string]any{keyHandle: []any{proxy}}},
		})
		route, err := json.Marshal(map[string]any{
			keyID: id, keyHandle: handle,
			"match":    []any{map[string]any{"host": []string{domain}}},
			"terminal": true,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("building route for %s: %w", domain, err)
		}
		routes[id] = route
	}

	indexHosts := []string{}
	if indexDir != "" {
		for _, host := range []string{cfg.TLD, generator.TLDLocalhost(cfg.TLD)} {
			if !slices.Contains(domains, host) && !slices.Contains(indexHosts, host) {
				indexHosts = append(indexHosts, host)
			}
		}
		indexRoute, err := json.Marshal(map[string]any{
			keyID: indexRouteID,
			keyHandle: []any{map[string]any{
				keyHandler: "subroute",
				"routes": []any{map[string]any{keyHandle: []any{
					map[string]any{keyHandler: "vars", "root": indexDir},
					map[string]any{keyHandler: "file_server", "hide": []string{"./Caddyfile"}},
				}}},
			}},
			"match":    []any{map[string]any{"host": indexHosts}},
			"terminal": true,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("building index route: %w", err)
		}
		routes[indexRouteID] = indexRoute
	}

	policies := make(map[string]json.RawMessage)
	subjects := append(slices.Clone(domains), indexHosts...)
	if len(subjects) > 0 {
		policy, err := json.Marshal(map[string]any{
			keyID: tlsPolicyID, "issuers": []any{map[string]any{"module": "internal"}}, "subjects": subjects,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("building TLS policy: %w", err)
		}
		policies[tlsPolicyID] = policy
	}
	return routes, policies, nil
}

func routeID(host string) string {
	return "devlocal-route-" + strings.ReplaceAll(host, ".", "-")
}
