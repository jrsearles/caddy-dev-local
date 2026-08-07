package generator

import (
	"context"
	"fmt"
	"os"
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
	TargetKind     targetKind
	GatewayHost    string
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

type targetKind int

const (
	targetUnreachable targetKind = iota
	targetDNS
	targetGateway
)

type selfInfo struct {
	id       string
	networks map[string]bool
	gateway  string
}

type probeFunc func(host string, ports []uint16, timeout time.Duration) (uint16, error)

type Generator struct {
	cfg          *config.Config
	docker       docker.Client
	mu           sync.RWMutex
	containers   map[string]*ContainerInfo
	probeFn      probeFunc
	selfHostname string
}

func NewGenerator(cfg *config.Config, dockerClient docker.Client) *Generator {
	return &Generator{
		cfg:          cfg,
		docker:       dockerClient,
		containers:   make(map[string]*ContainerInfo),
		probeFn:      ProbeHTTPPort,
		selfHostname: os.Getenv("HOSTNAME"),
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
	self := g.discoverSelf(containers)

	for i := range containers {
		c := &containers[i]
		if self.id != "" && c.ID == self.id {
			continue
		}
		if shouldSkip(c) {
			continue
		}
		if !g.cfg.Standalone && g.reachability(c, self) == targetUnreachable {
			continue
		}

		info := g.buildContainerInfo(c, self)
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
		if seen[id] {
			continue
		}
		if info.IsRunning {
			delete(g.containers, id)
		} else if !info.LastStopped.IsZero() && now.Sub(info.LastStopped) > g.cfg.StaleTTL {
			delete(g.containers, id)
		}
	}
}

func (g *Generator) discoverSelf(containers []container.Summary) selfInfo {
	info := selfInfo{networks: make(map[string]bool), gateway: "host.docker.internal"}
	if g.selfHostname == "" {
		return info
	}
	for i := range containers {
		c := &containers[i]
		if !isSelfContainer(c, g.selfHostname) {
			continue
		}
		info.id = c.ID
		if c.NetworkSettings == nil {
			break
		}
		for name, ep := range c.NetworkSettings.Networks {
			info.networks[name] = true
			if info.gateway == "host.docker.internal" && ep.Gateway.IsValid() {
				info.gateway = ep.Gateway.String()
			}
		}
		break
	}
	return info
}

func isSelfContainer(c *container.Summary, hostname string) bool {
	if hostname == "" {
		return false
	}
	if c.ID == hostname || docker.ContainerName(c) == hostname {
		return true
	}
	return strings.HasPrefix(c.ID, hostname) || strings.HasPrefix(hostname, c.ID)
}

func (g *Generator) reachability(c *container.Summary, self selfInfo) targetKind {
	if g.cfg.Standalone {
		return targetGateway
	}
	if c.NetworkSettings != nil {
		for name := range c.NetworkSettings.Networks {
			if self.networks[name] {
				return targetDNS
			}
		}
	}
	for _, p := range c.Ports {
		if p.PublicPort != 0 {
			return targetGateway
		}
	}
	return targetUnreachable
}

func firstNetworkIP(c *container.Summary) string {
	if c.NetworkSettings == nil {
		return ""
	}
	for _, ep := range c.NetworkSettings.Networks {
		if ep.IPAddress.IsValid() {
			return ep.IPAddress.String()
		}
	}
	return ""
}

func (g *Generator) buildContainerInfo(c *container.Summary, self selfInfo) *ContainerInfo {
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

	kind := g.reachability(c, self)
	ipAddress := ""
	switch kind {
	case targetDNS:
		if c.NetworkSettings != nil {
			for name, ep := range c.NetworkSettings.Networks {
				if self.networks[name] {
					ipAddress = ep.IPAddress.String()
					break
				}
			}
		}
	case targetGateway:
		ipAddress = firstNetworkIP(c)
	case targetUnreachable:
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
		TargetKind:     kind,
		GatewayHost:    self.gateway,
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

		host, ports := g.probeTarget(info)
		if len(ports) == 0 {
			continue
		}
		port, err := g.probeFn(host, ports, g.cfg.ProbeTimeout)
		if err == nil {
			info.SelectedPort = port
		}
	}
}

func (g *Generator) probeTarget(info *ContainerInfo) (string, []uint16) {
	var pubPorts []uint16
	for _, p := range info.Ports {
		if pub, ok := info.PublishedPorts[p]; ok {
			pubPorts = append(pubPorts, pub)
		}
	}

	switch {
	case g.cfg.Standalone:
		return "localhost", pubPorts
	case info.TargetKind == targetGateway:
		return info.GatewayHost, pubPorts
	default:
		return info.ContainerName, info.Ports
	}
}

func (g *Generator) hostFor(info *ContainerInfo) string {
	switch {
	case g.cfg.Standalone:
		return "localhost"
	case info.TargetKind == targetGateway:
		return info.GatewayHost
	default:
		return info.ContainerName
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
				port := cd.Port
				if g.cfg.Standalone || info.TargetKind == targetGateway {
					if pub, ok := info.PublishedPorts[cd.Port]; ok {
						port = pub
					}
				}
				addTarget(cd.Domain, fmt.Sprintf("%s:%d", g.hostFor(info), port))
			}
			continue
		}

		if info.SelectedPort == 0 {
			continue
		}

		domain := g.domainForContainer(info)
		target := fmt.Sprintf("%s:%d", g.hostFor(info), info.SelectedPort)
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
