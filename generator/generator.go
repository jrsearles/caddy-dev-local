package generator

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
)

const (
	localhostName   = "localhost"
	localhostSuffix = ".localhost"
)

type probeFunc func(host string, ports []uint16, timeout time.Duration) (uint16, error)

// DomainTargets computes the domain -> upstream-target map for all running
// containers, merging duplicate domains and registering the TLD and .localhost
// variants (or custom domains when set).
func DomainTargets(cfg *config.Config, containers []*discovery.ContainerInfo) map[string][]string {
	merged := make(map[string][]string)

	addTarget := func(domain, target string) {
		merged[domain] = append(merged[domain], target)
	}

	for _, info := range containers {
		if !info.IsRunning {
			continue
		}

		if custom := info.CustomDomains; len(custom) > 0 {
			for _, cd := range custom {
				addTarget(cd.Domain, fmt.Sprintf("%s:%d", hostFor(info), effectivePort(info, cd.Port)))
			}
			continue
		}

		if info.SelectedPort == 0 {
			continue
		}

		domain := domainForContainer(cfg, info)
		target := fmt.Sprintf("%s:%d", hostFor(info), info.SelectedPort)
		addTarget(domain, target)
		addTarget(domainForContainerLocalhost(info), target)
	}

	for d := range merged {
		slices.Sort(merged[d])
	}

	return merged
}

// Domains returns the sorted flattened list of registered domains for all
// running containers.
func Domains(cfg *config.Config, containers []*discovery.ContainerInfo) []string {
	seen := make(map[string]bool)
	var domains []string

	for _, info := range containers {
		if !info.IsRunning {
			continue
		}

		if custom := info.CustomDomains; len(custom) > 0 {
			for _, cd := range custom {
				if !seen[cd.Domain] {
					seen[cd.Domain] = true
					domains = append(domains, cd.Domain)
				}
			}
			continue
		}

		if !hasReachablePort(info) {
			continue
		}

		d := domainForContainer(cfg, info)
		if !seen[d] {
			seen[d] = true
			domains = append(domains, d)
		}
		ld := domainForContainerLocalhost(info)
		if !seen[ld] {
			seen[ld] = true
			domains = append(domains, ld)
		}
	}

	sort.Strings(domains)
	return domains
}

// PortSelector builds the discovery enricher that probes a running container's
// ports to select its HTTP port. The hosts binary does not register an
// enricher; the caddy entry point injects ProbeHTTPPort.
func PortSelector(cfg *config.Config, probeFn probeFunc) discovery.Enricher {
	return func(info *discovery.ContainerInfo) {
		if !info.IsRunning {
			return
		}

		if getLabel(info.Labels, "dev.local.domains") != "" {
			return
		}

		host, ports := probeTarget(info)
		if len(ports) == 0 {
			return
		}
		port, err := probeFn(host, ports, cfg.ProbeTimeout)
		if err == nil {
			info.SelectedPort = port
		}
	}
}

func probeTarget(info *discovery.ContainerInfo) (string, []uint16) {
	ports := make([]uint16, 0, len(info.Ports))
	for _, p := range info.Ports {
		ports = append(ports, effectivePort(info, p))
	}
	return hostFor(info), ports
}

func effectivePort(info *discovery.ContainerInfo, private uint16) uint16 {
	if pub, ok := info.PublishedPorts[private]; ok {
		return pub
	}
	return private
}

func hasReachablePort(info *discovery.ContainerInfo) bool {
	if info.SelectedPort > 0 || len(info.CustomDomains) > 0 {
		return true
	}
	return len(info.PublishedPorts) > 0
}

func hostFor(*discovery.ContainerInfo) string {
	return localhostName
}

func domainForContainer(cfg *config.Config, info *discovery.ContainerInfo) string {
	if info.IsCompose {
		return fmt.Sprintf("%s.%s.%s", info.Project, info.Service, cfg.TLD)
	}
	return fmt.Sprintf("%s.%s", info.ContainerName, cfg.TLD)
}

func domainForContainerLocalhost(info *discovery.ContainerInfo) string {
	if info.IsCompose {
		return fmt.Sprintf("%s.%s%s", info.Project, info.Service, localhostSuffix)
	}
	return fmt.Sprintf("%s%s", info.ContainerName, localhostSuffix)
}

// TLDLocalhost returns the .localhost alias for the TLD (e.g. dev.local → dev.localhost).
func TLDLocalhost(tld string) string {
	if strings.EqualFold(tld, localhostName) || strings.HasSuffix(strings.ToLower(tld), localhostSuffix) {
		return tld
	}
	return strings.Split(tld, ".")[0] + localhostSuffix
}

func getLabel(labels map[string]string, key string) string {
	if labels == nil {
		return ""
	}
	return labels[key]
}
