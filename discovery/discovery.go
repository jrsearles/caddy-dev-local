package discovery

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
	"go.uber.org/zap"

	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/docker"
)

type Status struct {
	LastError   string
	LastErrorAt time.Time
	LastRefresh time.Time
}

type CustomDomain struct {
	Port   uint16
	Domain string
}

type ContainerInfo struct {
	ContainerID    string
	ContainerName  string
	Image          string
	Project        string
	Service        string
	Ports          []uint16
	PublishedPorts map[uint16]uint16
	SelectedPort   uint16
	IsCompose      bool
	IsRunning      bool
	LastStopped    time.Time
	Created        time.Time
	Labels         map[string]string
	CustomDomains  []CustomDomain
	Health         string
	Networks       []string
}

type Update struct {
	Snapshot []*ContainerInfo
	Status   Status
}

// Discovery owns container state and drives the refresh loop. It publishes the
// latest complete state on each refresh.
type Discovery struct {
	cfg    *config.Config
	docker docker.Client
	logger *zap.Logger
	probe  PortProbe

	refreshMu     sync.Mutex
	mu            sync.RWMutex
	containers    map[string]*ContainerInfo
	selectedPorts map[string]uint16
	status        Status
	updates       chan Update
	started       atomic.Bool
	running       atomic.Bool
}

type Option func(*Discovery)

func WithPortProbe(probe PortProbe) Option {
	return func(d *Discovery) {
		d.probe = probe
	}
}

func New(cfg *config.Config, dockerClient docker.Client, logger *zap.Logger, options ...Option) *Discovery {
	d := &Discovery{
		cfg:           cfg,
		docker:        dockerClient,
		logger:        logger,
		containers:    make(map[string]*ContainerInfo),
		selectedPorts: make(map[string]uint16),
		updates:       make(chan Update, 1),
	}
	for _, option := range options {
		option(d)
	}
	return d
}

// Updates returns the latest authoritative states. Run closes the channel after
// its workers and any in-flight refresh have stopped.
func (d *Discovery) Updates() <-chan Update {
	return d.updates
}

// Refresh lists containers, replaces the stored state, and publishes an update.
// Once Run has been called, it also publishes an update while Run is active.
func (d *Discovery) Refresh(ctx context.Context) error {
	return d.refreshCycle(ctx)
}

func (d *Discovery) refreshCycle(ctx context.Context) error {
	d.refreshMu.Lock()
	defer d.refreshMu.Unlock()

	containers, err := d.docker.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		d.mu.Lock()
		d.status.LastError = err.Error()
		d.status.LastErrorAt = time.Now()
		update := Update{Snapshot: d.snapshotLocked(), Status: d.status}
		d.mu.Unlock()
		if d.running.Load() {
			d.publish(update)
		}
		return fmt.Errorf("listing containers: %w", err)
	}

	d.mu.RLock()
	previous := cloneContainerMap(d.containers)
	selectedPorts := cloneMap(d.selectedPorts)
	d.mu.RUnlock()

	next := d.refreshContainers(previous, containers)
	if err := d.selectPorts(ctx, next, selectedPorts); err != nil {
		return err
	}

	d.mu.Lock()
	d.containers = next
	d.selectedPorts = selectedPorts
	d.status.LastRefresh = time.Now()
	d.status.LastError = ""
	d.status.LastErrorAt = time.Time{}
	update := Update{Snapshot: d.snapshotLocked(), Status: d.status}
	d.mu.Unlock()

	if d.running.Load() {
		d.publish(update)
	}
	return nil
}

// Snapshot returns a sorted copy of the current container state.
func (d *Discovery) Snapshot() []*ContainerInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.snapshotLocked()
}

func (d *Discovery) snapshotLocked() []*ContainerInfo {
	result := make([]*ContainerInfo, 0, len(d.containers))
	for _, info := range d.containers {
		result = append(result, cloneContainerInfo(info))
	}
	slices.SortFunc(result, func(a, b *ContainerInfo) int {
		if a.ContainerName != b.ContainerName {
			return cmp.Compare(a.ContainerName, b.ContainerName)
		}
		return cmp.Compare(a.ContainerID, b.ContainerID)
	})
	return result
}

func (d *Discovery) Status() Status {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.status
}

func (d *Discovery) setError(msg string) {
	d.mu.Lock()
	d.status.LastError = msg
	d.status.LastErrorAt = time.Now()
	d.mu.Unlock()
}

func (d *Discovery) publish(update Update) { //nolint:gocritic
	// Updates are complete snapshots, so discard a buffered stale update rather
	// than making discovery wait for the consumer.
	select {
	case d.updates <- update:
	case <-d.updates:
		d.updates <- update
	}
}

func cloneContainerMap(infos map[string]*ContainerInfo) map[string]*ContainerInfo {
	result := make(map[string]*ContainerInfo, len(infos))
	for id, info := range infos {
		result[id] = cloneContainerInfo(info)
	}
	return result
}

func cloneContainerInfo(info *ContainerInfo) *ContainerInfo {
	if info == nil {
		return nil
	}
	result := *info
	result.Ports = slices.Clone(info.Ports)
	result.PublishedPorts = cloneMap(info.PublishedPorts)
	result.Labels = cloneMap(info.Labels)
	result.CustomDomains = slices.Clone(info.CustomDomains)
	result.Networks = slices.Clone(info.Networks)
	return &result
}

func cloneMap[K comparable, V any](src map[K]V) map[K]V {
	if src == nil {
		return nil
	}
	result := make(map[K]V, len(src))
	maps.Copy(result, src)
	return result
}

func (d *Discovery) refreshContainers(current map[string]*ContainerInfo, containers []container.Summary) map[string]*ContainerInfo {
	seen := make(map[string]bool)
	now := time.Now()

	for i := range containers {
		c := &containers[i]
		if shouldSkip(c) {
			continue
		}

		info := d.buildContainerInfo(c)
		if info == nil {
			continue
		}

		seen[info.ContainerID] = true

		prev, existed := current[info.ContainerID]

		if containers[i].State == "running" {
			info.IsRunning = true
			info.LastStopped = time.Time{}
		} else {
			info.IsRunning = false
			if existed {
				info.Ports = prev.Ports
				info.PublishedPorts = prev.PublishedPorts
				if prev.IsRunning {
					info.LastStopped = now
				} else {
					info.LastStopped = prev.LastStopped
				}
			} else {
				info.LastStopped = now
			}
		}

		current[info.ContainerID] = info
	}

	for id, info := range current {
		if seen[id] {
			continue
		}
		if info.IsRunning {
			delete(current, id)
		} else if !info.LastStopped.IsZero() && now.Sub(info.LastStopped) > d.cfg.StaleTTL {
			delete(current, id)
		}
	}
	return current
}

func (d *Discovery) buildContainerInfo(c *container.Summary) *ContainerInfo {
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

	health := ""
	if c.Health != nil {
		health = string(c.Health.Status)
	}

	networks := make([]string, 0)
	if c.NetworkSettings != nil {
		for netName := range c.NetworkSettings.Networks {
			networks = append(networks, netName)
		}
		slices.Sort(networks)
	}

	info := &ContainerInfo{
		ContainerID:    c.ID,
		ContainerName:  name,
		Image:          c.Image,
		Project:        project,
		Service:        service,
		Ports:          ports,
		PublishedPorts: publishedPorts,
		Health:         health,
		Networks:       networks,
		IsCompose:      isCompose,
		Created:        time.Unix(c.Created, 0),
		Labels:         cloneMap(c.Labels),
		CustomDomains:  parseCustomDomains(c.Labels),
	}

	return info
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
		if !portSet[p.PrivatePort] {
			portSet[p.PrivatePort] = true
			ports = append(ports, p.PrivatePort)
		}
	}

	slices.Sort(ports)
	return ports
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

func getLabel(labels map[string]string, key string) string {
	if labels == nil {
		return ""
	}
	return labels[key]
}

func (d *Discovery) Run(ctx context.Context) {
	if !d.started.CompareAndSwap(false, true) {
		return
	}
	d.running.Store(true)
	go d.run(ctx)
}

func (d *Discovery) run(ctx context.Context) {
	msgCh, errCh := d.dockerEvents(ctx)
	_ = d.Refresh(ctx)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		d.watchEvents(ctx, msgCh, errCh)
	}()
	go func() {
		defer wg.Done()
		d.staleCleanup(ctx)
	}()
	if d.cfg.PollInterval > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.pollLoop(ctx)
		}()
	}
	wg.Wait()
	d.running.Store(false)
	d.refreshMu.Lock()
	close(d.updates)
	d.refreshMu.Unlock()
}

func (d *Discovery) dockerEvents(ctx context.Context) (<-chan events.Message, <-chan error) {
	f := client.Filters{}
	f.Add("type", "container")
	f.Add("type", "network")
	return d.docker.Events(ctx, client.EventsListOptions{Filters: f})
}

func (d *Discovery) watchEvents(ctx context.Context, msgCh <-chan events.Message, errCh <-chan error) {
	d.logger.Info("watching docker events")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		d.streamEvents(ctx, msgCh, errCh)

		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
		msgCh, errCh = d.dockerEvents(ctx)
	}
}

func (d *Discovery) streamEvents(ctx context.Context, msgCh <-chan events.Message, errCh <-chan error) {
	throttle := time.NewTimer(100 * time.Millisecond)
	defer throttle.Stop()

	pending := false
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-msgCh:
			if !ok {
				d.logger.Warn("event stream closed, reconnecting in 30s")
				d.setError("Docker event stream closed, reconnecting")
				return
			} else if shouldRefresh(&event) {
				if !pending {
					pending = true
					throttle.Reset(100 * time.Millisecond)
				}
			}
		case err, ok := <-errCh:
			if !ok {
				d.logger.Warn("event stream error channel closed, reconnecting in 30s")
				d.setError("Docker event stream error channel closed, reconnecting")
				return
			}
			if err != nil {
				d.logger.Error("event stream error, reconnecting in 30s", zap.Error(err))
				d.setError("Docker event stream error: " + err.Error())
				return
			}
		case <-throttle.C:
			if pending {
				pending = false
				_ = d.Refresh(ctx)
			}
		}
	}
}

func shouldRefresh(event *events.Message) bool {
	switch event.Type {
	case events.ContainerEventType:
		switch event.Action {
		case events.ActionStart, events.ActionStop, events.ActionDie, events.ActionDestroy, events.ActionCreate:
			return true
		default:
			return false
		}
	case events.NetworkEventType:
		switch event.Action {
		case events.ActionConnect, events.ActionDisconnect:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func (d *Discovery) staleCleanup(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = d.Refresh(ctx)
			d.logger.Debug("stale cleanup completed")
		}
	}
}

func (d *Discovery) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = d.Refresh(ctx)
		}
	}
}
