package discovery

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"slices"
	"time"
)

const probeUserAgent = "DevLocal-Server-Detection"

var commonHTTPPorts = []uint16{80, 8080, 443, 8443}

type PortProbe func(context.Context, string, []uint16, time.Duration) (uint16, error)

func (d *Discovery) selectPorts(ctx context.Context, containers map[string]*ContainerInfo, cache map[string]uint16) error {
	infos := make([]*ContainerInfo, 0, len(containers))
	for _, info := range containers {
		infos = append(infos, info)
	}
	slices.SortFunc(infos, func(a, b *ContainerInfo) int {
		if a.ContainerName < b.ContainerName {
			return -1
		}
		if a.ContainerName > b.ContainerName {
			return 1
		}
		if a.ContainerID < b.ContainerID {
			return -1
		}
		if a.ContainerID > b.ContainerID {
			return 1
		}
		return 0
	})
	for _, info := range infos {
		id := info.ContainerID
		if !info.IsRunning {
			info.SelectedPort = cache[id]
			continue
		}

		info.SelectedPort = 0
		if d.probe == nil || getLabel(info.Labels, "dev.local.domains") != "" {
			continue
		}
		ports := publishedPorts(info)
		if len(ports) == 0 {
			continue
		}
		port, err := d.probe(ctx, "localhost", ports, d.cfg.ProbeTimeout)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			info.SelectedPort = port
			cache[id] = port
		}
	}
	for id := range cache {
		if _, ok := containers[id]; !ok {
			delete(cache, id)
		}
	}
	return nil
}

func publishedPorts(info *ContainerInfo) []uint16 {
	ports := make([]uint16, 0, len(info.PublishedPorts))
	seen := make(map[uint16]bool, len(info.PublishedPorts))
	for _, private := range info.Ports {
		public, ok := info.PublishedPorts[private]
		if ok && !seen[public] {
			seen[public] = true
			ports = append(ports, public)
		}
	}
	return ports
}

func preferPorts(ports []uint16) []uint16 {
	if len(ports) <= 1 {
		return ports
	}

	var preferred, rest []uint16
	for _, p := range commonHTTPPorts {
		if slices.Contains(ports, p) {
			preferred = append(preferred, p)
		}
	}
	for _, p := range ports {
		if !slices.Contains(commonHTTPPorts, p) {
			rest = append(rest, p)
		}
	}
	return append(preferred, rest...)
}

func ProbeHTTPPort(ctx context.Context, host string, ports []uint16, timeout time.Duration) (uint16, error) {
	if len(ports) == 0 {
		return 0, fmt.Errorf("no ports to probe")
	}

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: timeout}).DialContext,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // TLS verification intentionally skipped for local HTTP port probing
			},
			DisableKeepAlives:     true,
			MaxIdleConns:          1,
			IdleConnTimeout:       timeout,
			TLSHandshakeTimeout:   timeout,
			ResponseHeaderTimeout: timeout,
		},
	}

	for _, port := range preferPorts(ports) {
		url := fmt.Sprintf("http://%s:%d/", host, port)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", probeUserAgent)

		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			continue
		}
		resp.Body.Close()
		if resp.StatusCode > 0 {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no HTTP port found")
}
