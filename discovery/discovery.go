package discovery

import (
	"cmp"
	"context"
	"fmt"
	"reflect"
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

// Delta describes the batched changes for a single refresh cycle. It is
// dispatched synchronously to all subscribers in registration order.
type Delta struct {
	Added, Updated, Removed []*ContainerInfo
	Snapshot                []*ContainerInfo
	Status                  Status
}

// Subscriber receives a Delta for every refresh cycle once Discovery is running.
type Subscriber func(Delta)

// Discovery owns container state and drives the diff loop. It publishes
// batched deltas to subscribers and supports a pluggable enricher (port probe)
// that runs under the state lock during refresh. It is Caddy-free.
type Discovery struct {
	cfg       *config.Config
	docker    docker.Client
	logger    *zap.Logger
	enrichers []Enricher

	refreshMu   sync.Mutex
	mu          sync.RWMutex
	containers  map[string]*ContainerInfo
	status      Status
	subscribers []Subscriber
	running     atomic.Bool
}

// Enricher augments a freshly built ContainerInfo under the state lock. The
// caddy entry point injects the HTTP port probe; the hosts binary injects none.
type Enricher func(*ContainerInfo)

func New(cfg *config.Config, dockerClient docker.Client, logger *zap.Logger) *Discovery {
	return &Discovery{
		cfg:        cfg,
		docker:     dockerClient,
		logger:     logger,
		containers: make(map[string]*ContainerInfo),
	}
}

// SetEnricher injects the optional port-probe enricher. Register before Run.
func (d *Discovery) SetEnricher(fn Enricher) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.enrichers = nil
	if fn != nil {
		d.enrichers = append(d.enrichers, fn)
	}
}

// AddEnricher appends an enricher to the ordered chain. Register before Run.
func (d *Discovery) AddEnricher(fn Enricher) {
	if fn == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.enrichers = append(d.enrichers, fn)
}

// Subscribe registers a delta subscriber. Register before Run.
func (d *Discovery) Subscribe(fn Subscriber) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.subscribers = append(d.subscribers, fn)
}

// Refresh lists containers, diffs the stored state, runs the enricher, and
// returns the resulting Delta. Once Run has been called the Delta is also
// dispatched to subscribers.
func (d *Discovery) Refresh(ctx context.Context) error {
	_, err := d.refreshCycle(ctx)
	return err
}

func (d *Discovery) refreshCycle(ctx context.Context) (Delta, error) {
	d.refreshMu.Lock()
	defer d.refreshMu.Unlock()

	containers, err := d.docker.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		d.mu.Lock()
		d.status.LastError = err.Error()
		d.status.LastErrorAt = time.Now()
		delta := Delta{Snapshot: d.snapshotLocked(), Status: d.status}
		d.mu.Unlock()
		if d.running.Load() {
			d.dispatch(&delta)
		}
		return delta, fmt.Errorf("listing containers: %w", err)
	}

	d.mu.Lock()
	delta := d.refreshLocked(containers)
	d.status.LastRefresh = time.Now()
	d.status.LastError = ""
	d.status.LastErrorAt = time.Time{}
	delta.Status = d.status
	delta.Snapshot = d.snapshotLocked()
	delta = CloneDelta(&delta)
	d.mu.Unlock()

	if d.running.Load() {
		d.dispatch(&delta)
	}
	return delta, nil
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

func (d *Discovery) dispatch(delta *Delta) {
	d.mu.RLock()
	subs := slices.Clone(d.subscribers)
	d.mu.RUnlock()
	for _, s := range subs {
		s(CloneDelta(delta))
	}
}

func CloneDelta(delta *Delta) Delta {
	if delta == nil {
		return Delta{}
	}
	result := *delta
	result.Added = cloneContainerInfos(delta.Added)
	result.Updated = cloneContainerInfos(delta.Updated)
	result.Removed = cloneContainerInfos(delta.Removed)
	result.Snapshot = cloneContainerInfos(delta.Snapshot)
	return result
}

func cloneContainerInfos(infos []*ContainerInfo) []*ContainerInfo {
	if infos == nil {
		return nil
	}
	result := make([]*ContainerInfo, len(infos))
	for i, info := range infos {
		result[i] = cloneContainerInfo(info)
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
	for key, value := range src {
		result[key] = value
	}
	return result
}

func (d *Discovery) refreshLocked(containers []container.Summary) Delta {
	seen := make(map[string]bool)
	now := time.Now()

	var added, updated, removed []*ContainerInfo

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

		prev, existed := d.containers[info.ContainerID]

		if containers[i].State == "running" {
			info.IsRunning = true
			info.LastStopped = time.Time{}
		} else {
			info.IsRunning = false
			if existed {
				info.Ports = prev.Ports
				info.PublishedPorts = prev.PublishedPorts
				info.SelectedPort = prev.SelectedPort
				if prev.IsRunning {
					info.LastStopped = now
				} else {
					info.LastStopped = prev.LastStopped
				}
			} else {
				info.LastStopped = now
			}
		}

		if info.IsRunning {
			for _, enricher := range d.enrichers {
				enricher(info)
			}
		}

		d.containers[info.ContainerID] = info

		if !existed {
			added = append(added, info)
		} else if !reflect.DeepEqual(prev, info) {
			updated = append(updated, info)
		}
	}

	for id, info := range d.containers {
		if seen[id] {
			continue
		}
		if info.IsRunning {
			delete(d.containers, id)
			removed = append(removed, info)
		} else if !info.LastStopped.IsZero() && now.Sub(info.LastStopped) > d.cfg.StaleTTL {
			delete(d.containers, id)
			removed = append(removed, info)
		}
	}

	return Delta{Added: added, Updated: updated, Removed: removed}
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
	if !d.running.CompareAndSwap(false, true) {
		return
	}
	go d.run(ctx)
}

func (d *Discovery) run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		d.watchEvents(ctx)
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
}

func (d *Discovery) watchEvents(ctx context.Context) {
	d.logger.Info("watching docker events")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		f := client.Filters{}
		f.Add("type", "container")
		f.Add("type", "network")

		msgCh, errCh := d.docker.Events(ctx, client.EventsListOptions{
			Filters: f,
		})

		d.streamEvents(ctx, msgCh, errCh)

		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
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
