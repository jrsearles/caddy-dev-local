package generator

import (
	"context"
	"fmt"
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
	Project        string
	Service        string
	Ports          []uint16
	PublishedPorts map[uint16]uint16
	SelectedPort   uint16
	IsCompose      bool
	IsRunning      bool
	LastStopped    time.Time
	Labels         map[string]string
}

type CustomDomain struct {
	Port   uint16
	Domain string
}

type Generator struct {
	cfg        *config.Config
	docker     docker.Client
	mu         sync.RWMutex
	containers map[string]*ContainerInfo
}

func NewGenerator(cfg *config.Config, dockerClient docker.Client) *Generator {
	return &Generator{
		cfg:        cfg,
		docker:     dockerClient,
		containers: make(map[string]*ContainerInfo),
	}
}

func (g *Generator) Refresh(ctx context.Context) error {
	containers, err := g.docker.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	seen := make(map[string]bool)
	now := time.Now()

	for _, c := range containers {
		if !docker.HasNetwork(c, g.cfg.IngressNetwork) {
			continue
		}

		info := g.buildContainerInfo(c)
		if info == nil {
			continue
		}

		seen[info.ContainerID] = true

		if c.State == "running" {
			info.IsRunning = true
			info.LastStopped = time.Time{}
		} else {
			if info.IsRunning {
				info.LastStopped = now
			}
			info.IsRunning = false
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

	return nil
}

func (g *Generator) buildContainerInfo(c container.Summary) *ContainerInfo {
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

	info := &ContainerInfo{
		ContainerID:    c.ID,
		ContainerName:  name,
		Project:        project,
		Service:        service,
		Ports:          ports,
		PublishedPorts: publishedPorts,
		IsCompose:      isCompose,
		Labels:         c.Labels,
	}

	return info
}

func (g *Generator) SelectPorts(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

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

			if len(pubPorts) == 0 {
				continue
			} else if len(pubPorts) == 1 {
				info.SelectedPort = pubPorts[0]
			} else {
				port, err := ProbeHTTPPort(pubPorts, g.cfg.ProbeTimeout)
				if err == nil {
					info.SelectedPort = port
				}
			}
		} else {
			if len(info.Ports) == 1 {
				info.SelectedPort = info.Ports[0]
			} else if len(info.Ports) > 1 {
				port, err := ProbeHTTPPort(info.Ports, g.cfg.ProbeTimeout)
				if err == nil {
					info.SelectedPort = port
				}
			}
		}
	}

	return nil
}

func (g *Generator) GenerateCaddyfile() string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var sb strings.Builder

	for _, info := range g.containers {
		if !info.IsRunning {
			continue
		}

		if customDomains := g.customDomains(info); len(customDomains) > 0 {
			for _, cd := range customDomains {
				sb.WriteString(fmt.Sprintf("%s {\n", cd.Domain))
				sb.WriteString("    tls internal\n")
				if g.cfg.Standalone {
					if pub, ok := info.PublishedPorts[cd.Port]; ok {
						sb.WriteString(fmt.Sprintf("    reverse_proxy localhost:%d\n", pub))
					} else {
						sb.WriteString(fmt.Sprintf("    reverse_proxy localhost:%d\n", cd.Port))
					}
				} else {
					sb.WriteString(fmt.Sprintf("    reverse_proxy %s:%d\n", info.ContainerName, cd.Port))
				}
				sb.WriteString("}\n\n")
			}
			continue
		}

		if info.SelectedPort == 0 {
			continue
		}

		domain := g.domainForContainer(info)
		sb.WriteString(fmt.Sprintf("%s {\n", domain))
		sb.WriteString("    tls internal\n")
		if g.cfg.Standalone {
			sb.WriteString(fmt.Sprintf("    reverse_proxy localhost:%d\n", info.SelectedPort))
		} else {
			sb.WriteString(fmt.Sprintf("    reverse_proxy %s:%d\n", info.ContainerName, info.SelectedPort))
		}
		sb.WriteString("}\n\n")
	}

	return sb.String()
}

func (g *Generator) domainForContainer(info *ContainerInfo) string {
	if info.IsCompose {
		return fmt.Sprintf("%s.%s.%s", info.Project, info.Service, g.cfg.TLD)
	}
	return fmt.Sprintf("%s.%s", info.ContainerName, g.cfg.TLD)
}

func (g *Generator) customDomains(info *ContainerInfo) []CustomDomain {
	label := getLabel(info.Labels, "dev.local.domains")
	if label == "" {
		return nil
	}

	var domains []CustomDomain
	entries := strings.Split(label, ";")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			continue
		}

		var port uint16
		fmt.Sscanf(parts[0], "%d", &port)
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

func (g *Generator) Containers() []*ContainerInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var result []*ContainerInfo
	for _, info := range g.containers {
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool {
		return g.domainForContainer(result[i]) < g.domainForContainer(result[j])
	})
	return result
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

func shouldSkip(c container.Summary) bool {
	val := docker.LabelValue(c, "dev.local")
	switch strings.ToLower(val) {
	case "false", "0", "no":
		return true
	}
	return false
}

func extractPorts(c container.Summary) []uint16 {
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

	sort.Slice(ports, func(i, j int) bool {
		return ports[i] < ports[j]
	})

	return ports
}

func getLabel(labels map[string]string, key string) string {
	if labels == nil {
		return ""
	}
	return labels[key]
}
