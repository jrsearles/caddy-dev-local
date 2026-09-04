package ui

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/jrsearles/caddy-dev-local/discovery"
	"github.com/jrsearles/caddy-dev-local/generator"
)

const (
	indexHTMLName = "index.html"
	indexCSSName  = "index.css"
	versionName   = "version.json"
)

type ConfigSource interface {
	RunningConfig(context.Context) (string, error)
}

type Plugin struct {
	tld       string
	outputDir string
	source    ConfigSource

	mu           sync.Mutex
	cachedConfig string
}

func New(tld, outputDir string, source ConfigSource) *Plugin {
	return &Plugin{tld: tld, outputDir: outputDir, source: source}
}

func (p *Plugin) Name() string {
	return "ui"
}

func (p *Plugin) Apply(ctx context.Context, delta discovery.Delta) error { //nolint:gocritic
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.outputDir == "" {
		return fmt.Errorf("ui plugin: output directory is empty")
	}
	if p.source != nil {
		if runningConfig, err := p.source.RunningConfig(ctx); err == nil {
			p.cachedConfig = runningConfig
		}
	}

	snapshot := canonicalSnapshot(delta.Snapshot)
	lastRefresh := int64(0)
	if !delta.Status.LastRefresh.IsZero() {
		lastRefresh = delta.Status.LastRefresh.Unix()
	}
	page := generator.GenerateIndexPage(p.tld, snapshot, p.cachedConfig, delta.Status.LastError, lastRefresh)
	fingerprint := fingerprint(page, generator.IndexCSS)

	if err := os.MkdirAll(p.outputDir, 0755); err != nil {
		return fmt.Errorf("creating UI output directory: %w", err)
	}
	if err := os.Chmod(p.outputDir, 0755); err != nil {
		return fmt.Errorf("setting UI output directory permissions: %w", err)
	}
	if err := writeIfChanged(filepath.Join(p.outputDir, indexHTMLName), []byte(page)); err != nil {
		return err
	}
	if err := writeIfChanged(filepath.Join(p.outputDir, indexCSSName), []byte(generator.IndexCSS)); err != nil {
		return err
	}
	version, err := json.Marshal(struct {
		Version string `json:"v"`
	}{Version: fingerprint})
	if err != nil {
		return fmt.Errorf("encoding UI version: %w", err)
	}
	if err := writeIfChanged(filepath.Join(p.outputDir, versionName), version); err != nil {
		return err
	}
	return nil
}

func (p *Plugin) Cleanup(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.outputDir == "" {
		return fmt.Errorf("ui plugin: output directory is empty")
	}

	var cleanupErrors []error
	for _, name := range []string{indexHTMLName, indexCSSName, versionName} {
		if err := os.Remove(filepath.Join(p.outputDir, name)); err != nil && !os.IsNotExist(err) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("removing %s: %w", name, err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func canonicalSnapshot(snapshot []*discovery.ContainerInfo) []*discovery.ContainerInfo {
	result := make([]*discovery.ContainerInfo, 0, len(snapshot))
	for _, info := range snapshot {
		if info == nil {
			continue
		}
		clone := *info
		clone.Ports = slices.Clone(info.Ports)
		slices.Sort(clone.Ports)
		clone.Networks = slices.Clone(info.Networks)
		slices.Sort(clone.Networks)
		clone.CustomDomains = slices.Clone(info.CustomDomains)
		slices.SortFunc(clone.CustomDomains, func(a, b discovery.CustomDomain) int {
			if n := cmp.Compare(a.Domain, b.Domain); n != 0 {
				return n
			}
			return cmp.Compare(a.Port, b.Port)
		})
		result = append(result, &clone)
	}
	slices.SortFunc(result, func(a, b *discovery.ContainerInfo) int {
		return cmp.Compare(a.ContainerID, b.ContainerID)
	})
	return result
}

func fingerprint(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeIfChanged(path string, content []byte) error {
	if existing, err := os.ReadFile(path); err == nil && slices.Equal(existing, content) {
		if err := os.Chmod(path, 0644); err != nil {
			return fmt.Errorf("setting permissions on %s: %w", filepath.Base(path), err)
		}
		return nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".devlocal-*")
	if err != nil {
		return fmt.Errorf("creating temporary %s: %w", filepath.Base(path), err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return fmt.Errorf("setting permissions on %s: %w", filepath.Base(path), err)
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publishing %s: %w", filepath.Base(path), err)
	}
	return nil
}
