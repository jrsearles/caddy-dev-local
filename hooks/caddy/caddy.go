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
	tlsPolicyID = "devlocal-tls"
	keyID       = "@id"
	keyHandle   = "handle"
	keyHandler  = "handler"
)

type Hook struct {
	cfg    *config.Config
	client *caddyapi.Client
}

// New returns a Hook that keeps Caddy in sync with the discovered containers:
// it translates each snapshot into reverse-proxy routes and an internal-issuer
// TLS policy, and reconciles them through the admin API.
func New(cfg *config.Config, client *caddyapi.Client) *Hook {
	return &Hook{cfg: cfg, client: client}
}

func (p *Hook) Name() string {
	return "caddy"
}

func (p *Hook) Apply(ctx context.Context, update discovery.Update) error { //nolint:gocritic
	if p.cfg == nil {
		return fmt.Errorf("caddy hook: config is nil")
	}
	if p.client == nil {
		return fmt.Errorf("caddy hook: API client is nil")
	}
	if update.Status.LastRefresh.IsZero() && update.Status.LastError != "" {
		return nil
	}
	targets := generator.DomainTargets(p.cfg, update.Snapshot)
	routes, policies, err := buildConfig(p.cfg, targets)
	if err != nil {
		return err
	}
	return p.client.Reconcile(ctx, routes, policies, owned)
}

// owned reports identifiers this hook manages: per-container proxy routes and
// the container TLS policy. Resources owned by other devlocal components (such
// as the index route) are left to their owners.
func owned(id string) bool {
	return strings.HasPrefix(id, "devlocal-route-") || id == tlsPolicyID
}

func (p *Hook) Cleanup(ctx context.Context) error {
	if p.client == nil {
		return fmt.Errorf("caddy hook: API client is nil")
	}
	return p.client.Cleanup(ctx)
}

func buildConfig(cfg *config.Config, targets map[string][]string) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	domains := make([]string, 0, len(targets))
	for domain := range targets {
		domains = append(domains, domain)
	}
	slices.Sort(domains)

	routes := make(map[string]json.RawMessage, len(domains))
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

	policies := make(map[string]json.RawMessage)
	if len(domains) > 0 {
		policy, err := json.Marshal(map[string]any{
			keyID: tlsPolicyID, "issuers": []any{map[string]any{"module": "internal"}}, "subjects": domains,
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
