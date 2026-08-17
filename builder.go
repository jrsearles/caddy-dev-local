package caddydevlocal

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/jsearles/caddy-dev-local/generator"
)

const (
	devlocalServerName  = "srv0"
	devlocalTLSPolicyID = "devlocal-tls"
)

const (
	keyHandler = "handler"
	keyHandle  = "handle"
	keyRoutes  = "routes"
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

func buildDevlocalConfig(tld, indexDir string, targets map[string][]string) (*devlocalConfig, error) {
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

		route, err := buildReverseProxyRoute(host, upstreams)
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

func buildReverseProxyRoute(host string, targets []string) (json.RawMessage, error) {
	upstreams := make([]any, 0, len(targets))
	for _, t := range targets {
		upstreams = append(upstreams, map[string]any{"dial": t})
	}

	route := map[string]any{
		"@id": devlocalRouteID(host),
		keyHandle: []any{
			map[string]any{
				keyHandler: "subroute",
				keyRoutes: []any{
					map[string]any{
						keyHandle: []any{
							map[string]any{
								keyHandler:  "reverse_proxy",
								"upstreams": upstreams,
							},
						},
					},
				},
			},
		},
		"match": []any{
			map[string]any{"host": []any{host}},
		},
		"terminal": true,
	}
	return json.Marshal(route)
}

func buildIndexRoute(tld, indexDir string, containerDomains []string) (json.RawMessage, error) {
	hosts := []any{tld}
	if alias := generator.TLDLocalhost(tld); alias != tld && !slices.Contains(containerDomains, alias) {
		hosts = append(hosts, alias)
	}

	route := map[string]any{
		keyHandle: []any{
			map[string]any{
				keyHandler: "subroute",
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
