package generator

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jsearles/caddy-dev-local/config"
	"github.com/jsearles/caddy-dev-local/docker"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type ContainerInfo struct {
	ContainerID    string
	ContainerName  string
	Image          string
	Project        string
	Service        string
	IPAddress      string
	Ports          []uint16
	PublishedPorts map[uint16]uint16
	SelectedPort   uint16
	IsCompose      bool
	IsRunning      bool
	LastStopped    time.Time
	Created        time.Time
	Labels         map[string]string
	CustomDomains  []CustomDomain
}

type CustomDomain struct {
	Port   uint16
	Domain string
}

type probeFunc func(host string, ports []uint16, timeout time.Duration) (uint16, error)

type Generator struct {
	cfg        *config.Config
	docker     docker.Client
	mu         sync.RWMutex
	containers map[string]*ContainerInfo
	probeFn    probeFunc
}

func NewGenerator(cfg *config.Config, dockerClient docker.Client) *Generator {
	return &Generator{
		cfg:        cfg,
		docker:     dockerClient,
		containers: make(map[string]*ContainerInfo),
		probeFn:    ProbeHTTPPort,
	}
}

func (g *Generator) Refresh(ctx context.Context) error {
	containers, err := g.docker.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.refreshLocked(containers)
	return nil
}

func (g *Generator) RefreshAndSelect(ctx context.Context) error {
	containers, err := g.docker.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.refreshLocked(containers)
	g.selectPortsLocked()
	return nil
}

func (g *Generator) refreshLocked(containers []container.Summary) {
	seen := make(map[string]bool)
	now := time.Now()

	for i := range containers {
		c := &containers[i]
		if !g.cfg.Standalone && !docker.HasNetwork(c, g.cfg.IngressNetwork) {
			continue
		}

		info := g.buildContainerInfo(c)
		if info == nil {
			continue
		}

		seen[info.ContainerID] = true

		prev, existed := g.containers[info.ContainerID]

		if containers[i].State == "running" {
			info.IsRunning = true
			info.LastStopped = time.Time{}
		} else {
			info.IsRunning = false
			if existed {
				info.Ports = prev.Ports
				info.PublishedPorts = prev.PublishedPorts
				info.SelectedPort = prev.SelectedPort
				info.IPAddress = prev.IPAddress
				if prev.IsRunning {
					info.LastStopped = now
				} else {
					info.LastStopped = prev.LastStopped
				}
			} else {
				info.LastStopped = now
			}
		}

		g.containers[info.ContainerID] = info
	}

	for id, info := range g.containers {
		if !seen[id] && !info.IsRunning {
			if !info.LastStopped.IsZero() && now.Sub(info.LastStopped) > g.cfg.StaleTTL {
				delete(g.containers, id)
			}
		}
	}
}

func (g *Generator) buildContainerInfo(c *container.Summary) *ContainerInfo {
	if shouldSkip(c) {
		return nil
	}

	name := docker.ContainerName(c)
	project := docker.ComposeProject(c)
	service := docker.ComposeService(c)
	isCompose := project != "" && service != ""

	ports := extractPorts(c)

	publishedPorts := make(map[uint16]uint16)
	for _, p := range c.Ports {
		if p.PublicPort != 0 {
			publishedPorts[p.PrivatePort] = p.PublicPort
		}
	}

	ipAddress := ""
	if c.NetworkSettings != nil {
		if ep, ok := c.NetworkSettings.Networks[g.cfg.IngressNetwork]; ok {
			ipAddress = ep.IPAddress.String()
		}
	}

	info := &ContainerInfo{
		ContainerID:    c.ID,
		ContainerName:  name,
		Image:          c.Image,
		Project:        project,
		Service:        service,
		IPAddress:      ipAddress,
		Ports:          ports,
		PublishedPorts: publishedPorts,
		IsCompose:      isCompose,
		Created:        time.Unix(c.Created, 0),
		Labels:         c.Labels,
		CustomDomains:  parseCustomDomains(c.Labels),
	}

	return info
}

func (g *Generator) SelectPorts(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.selectPortsLocked()
	return nil
}

func (g *Generator) selectPortsLocked() {
	for _, info := range g.containers {
		if !info.IsRunning {
			continue
		}

		if getLabel(info.Labels, "dev.local.domains") != "" {
			continue
		}

		if g.cfg.Standalone {
			var pubPorts []uint16
			for _, p := range info.Ports {
				if pub, ok := info.PublishedPorts[p]; ok {
					pubPorts = append(pubPorts, pub)
				}
			}

			switch {
			case len(pubPorts) == 0:
				continue
			default:
				port, err := g.probeFn("localhost", pubPorts, g.cfg.ProbeTimeout)
				if err == nil {
					info.SelectedPort = port
				}
			}
		} else if len(info.Ports) > 0 {
			port, err := g.probeFn(info.ContainerName, info.Ports, g.cfg.ProbeTimeout)
			if err == nil {
				info.SelectedPort = port
			}
		}
	}
}

func (g *Generator) DomainTargets() map[string][]string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	merged := make(map[string][]string)

	addTarget := func(domain, target string) {
		merged[domain] = append(merged[domain], target)
	}

	for _, info := range g.containers {
		if !info.IsRunning {
			continue
		}

		if customDomains := g.customDomains(info); len(customDomains) > 0 {
			for _, cd := range customDomains {
				var target string
				if g.cfg.Standalone {
					if pub, ok := info.PublishedPorts[cd.Port]; ok {
						target = fmt.Sprintf("localhost:%d", pub)
					} else {
						target = fmt.Sprintf("localhost:%d", cd.Port)
					}
				} else {
					target = fmt.Sprintf("%s:%d", info.ContainerName, cd.Port)
				}
				addTarget(cd.Domain, target)
			}
			continue
		}

		if info.SelectedPort == 0 {
			continue
		}

		domain := g.domainForContainer(info)
		var target string
		if g.cfg.Standalone {
			target = fmt.Sprintf("localhost:%d", info.SelectedPort)
		} else {
			target = fmt.Sprintf("%s:%d", info.ContainerName, info.SelectedPort)
		}
		addTarget(domain, target)

		if g.cfg.Standalone {
			addTarget(g.domainForContainerLocalhost(info), target)
		}
	}

	for d := range merged {
		slices.Sort(merged[d])
	}

	return merged
}

func (g *Generator) domainForContainer(info *ContainerInfo) string {
	if info.IsCompose {
		return fmt.Sprintf("%s.%s.%s", info.Project, info.Service, g.cfg.TLD)
	}
	return fmt.Sprintf("%s.%s", info.ContainerName, g.cfg.TLD)
}

func (g *Generator) domainForContainerLocalhost(info *ContainerInfo) string {
	if info.IsCompose {
		return fmt.Sprintf("%s.%s.localhost", info.Project, info.Service)
	}
	return fmt.Sprintf("%s.localhost", info.ContainerName)
}

func parseCustomDomains(labels map[string]string) []CustomDomain {
	label := getLabel(labels, "dev.local.domains")
	if label == "" {
		return nil
	}

	var domains []CustomDomain
	entries := strings.SplitSeq(label, ";")
	for entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			continue
		}

		var port uint16
		fmt.Sscanf(parts[0], "%d", &port) //nolint:errcheck // port defaults to 0 on failure
		if port == 0 {
			continue
		}

		domain := strings.TrimSpace(parts[1])
		if domain == "" {
			continue
		}

		domains = append(domains, CustomDomain{Port: port, Domain: domain})
	}

	return domains
}

func (g *Generator) customDomains(info *ContainerInfo) []CustomDomain {
	return info.CustomDomains
}

func (g *Generator) Containers() []*ContainerInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()

	result := make([]*ContainerInfo, 0, len(g.containers))
	for _, info := range g.containers {
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool {
		return g.domainForContainer(result[i]) < g.domainForContainer(result[j])
	})
	return result
}

func (g *Generator) Domains() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	seen := make(map[string]bool)
	var domains []string

	for _, info := range g.containers {
		if !info.IsRunning {
			continue
		}

		if custom := g.customDomains(info); len(custom) > 0 {
			for _, cd := range custom {
				if !seen[cd.Domain] {
					seen[cd.Domain] = true
					domains = append(domains, cd.Domain)
				}
			}
			continue
		}

		if info.SelectedPort == 0 {
			if g.cfg.Standalone {
				if len(info.PublishedPorts) == 0 {
					continue
				}
			} else if len(info.Ports) == 0 {
				continue
			}
		}

		d := g.domainForContainer(info)
		if !seen[d] {
			seen[d] = true
			domains = append(domains, d)
		}
		if g.cfg.Standalone {
			ld := g.domainForContainerLocalhost(info)
			if !seen[ld] {
				seen[ld] = true
				domains = append(domains, ld)
			}
		}
	}

	sort.Strings(domains)
	return domains
}

func (g *Generator) StaleCleanup() {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	for id, info := range g.containers {
		if !info.IsRunning && !info.LastStopped.IsZero() && now.Sub(info.LastStopped) > g.cfg.StaleTTL {
			delete(g.containers, id)
		}
	}
}

func shouldSkip(c *container.Summary) bool {
	val := docker.LabelValue(c, "dev.local")
	switch strings.ToLower(val) {
	case "false", "0", "no": //nolint:goconst // string literals in switch
		return true
	}
	return false
}

func extractPorts(c *container.Summary) []uint16 {
	portSet := make(map[uint16]bool)
	var ports []uint16

	for _, p := range c.Ports {
		if p.PublicPort == 0 {
			if !portSet[p.PrivatePort] {
				portSet[p.PrivatePort] = true
				ports = append(ports, p.PrivatePort)
			}
		}
	}

	if len(ports) == 0 {
		for _, p := range c.Ports {
			if !portSet[p.PrivatePort] {
				portSet[p.PrivatePort] = true
				ports = append(ports, p.PrivatePort)
			}
		}
	}

	slices.Sort(ports)
	return ports
}

func getLabel(labels map[string]string, key string) string {
	if labels == nil {
		return ""
	}
	return labels[key]
}
