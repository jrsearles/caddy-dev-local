package caddydevlocal

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/jrsearles/caddy-dev-local/generator"
)

const (
	devlocalServerName  = "srv0"
	devlocalTLSPolicyID = "devlocal-tls"
)

const (
	keyHandler      = "handler"
	keyHandle       = "handle"
	keyRoutes       = "routes"
	handlerTracing  = "tracing"
	handlerSubroute = "subroute"
	handlerProxy    = "reverse_proxy"
)

type devlocalConfig struct {
	routes     map[string]json.RawMessage
	policies   map[string]json.RawMessage
	indexRoute json.RawMessage
}

type devlocalAutosave struct {
	Routes     map[string]json.RawMessage `json:"routes"`
	Policies   map[string]json.RawMessage `json:"policies"`
	IndexRoute json.RawMessage            `json:"index_route"`
}

func devlocalRouteID(host string) string {
	return "devlocal-route-" + strings.ReplaceAll(host, ".", "-")
}

func buildDevlocalConfig(tld, indexDir string, targets map[string][]string, tracing bool) (*devlocalConfig, error) {
	cfg := &devlocalConfig{
		routes:   make(map[string]json.RawMessage, len(targets)),
		policies: make(map[string]json.RawMessage),
	}

	domains := make([]string, 0, len(targets))
	for host := range targets {
		domains = append(domains, host)
	}
	slices.Sort(domains)

	indexRoute, err := buildIndexRoute(tld, indexDir, domains)
	if err != nil {
		return nil, fmt.Errorf("building index route: %w", err)
	}
	cfg.indexRoute = indexRoute

	subjects := make([]string, 0, len(domains))
	for _, host := range domains {
		upstreams := slices.Clone(targets[host])
		slices.Sort(upstreams)

		route, err := buildReverseProxyRoute(host, upstreams, tracing)
		if err != nil {
			return nil, fmt.Errorf("building route for %s: %w", host, err)
		}
		cfg.routes[devlocalRouteID(host)] = route
		subjects = append(subjects, host)
	}

	if len(subjects) > 0 {
		policy, err := buildTLSPolicy(subjects)
		if err != nil {
			return nil, fmt.Errorf("building TLS policy: %w", err)
		}
		cfg.policies[devlocalTLSPolicyID] = policy
	}

	return cfg, nil
}

func buildReverseProxyRoute(host string, targets []string, tracing bool) (json.RawMessage, error) {
	upstreams := make([]any, 0, len(targets))
	for _, t := range targets {
		upstreams = append(upstreams, map[string]any{"dial": t})
	}

	proxyHandler := map[string]any{
		keyHandler:  handlerProxy,
		"upstreams": upstreams,
	}

	var handle []any
	if tracing {
		handle = []any{
			map[string]any{keyHandler: handlerTracing, "span": "{http.request.method} {http.request.host}"},
			map[string]any{
				keyHandler: handlerSubroute,
				keyRoutes: []any{
					map[string]any{
						keyHandle: []any{proxyHandler},
					},
				},
			},
		}
	} else {
		handle = []any{
			map[string]any{
				keyHandler: handlerSubroute,
				keyRoutes: []any{
					map[string]any{
						keyHandle: []any{proxyHandler},
					},
				},
			},
		}
	}

	route := map[string]any{
		"@id":     devlocalRouteID(host),
		keyHandle: handle,
		"match": []any{
			map[string]any{"host": []any{host}},
		},
		"terminal": true,
	}
	return json.Marshal(route)
}

func buildIndexRoute(tld, indexDir string, containerDomains []string) (json.RawMessage, error) {
	var hosts []any
	if alias := generator.TLDLocalhost(tld); !slices.Contains(containerDomains, alias) {
		hosts = []any{alias}
	}

	route := map[string]any{
		keyHandle: []any{
			map[string]any{
				keyHandler: handlerSubroute,
				keyRoutes: []any{
					map[string]any{
						keyHandle: []any{
							map[string]any{keyHandler: "vars", "root": indexDir},
							map[string]any{keyHandler: "file_server", "hide": []any{"./Caddyfile", "./devlocal.json"}},
						},
					},
				},
			},
		},
		"match": []any{
			map[string]any{"host": hosts},
		},
		"terminal": true,
	}
	return json.Marshal(route)
}

func buildTLSPolicy(domains []string) (json.RawMessage, error) {
	sorted := slices.Clone(domains)
	slices.Sort(sorted)

	subjects := make([]any, 0, len(sorted))
	for _, d := range sorted {
		subjects = append(subjects, d)
	}

	policy := map[string]any{
		"@id": devlocalTLSPolicyID,
		"issuers": []any{
			map[string]any{"module": "internal"},
		},
		"subjects": subjects,
	}
	return json.Marshal(policy)
}
