package generator

import (
	"fmt"
	"net"
	"net/http"
	"slices"
	"time"
)

const probeUserAgent = "DevLocal-Server-Detection"

var commonHTTPPorts = []uint16{80, 8080, 443, 8443}

func preferPorts(ports []uint16) []uint16 {
	if len(ports) <= 1 {
		return ports
	}

	portSet := make(map[uint16]bool, len(ports))
	for _, p := range ports {
		portSet[p] = true
	}

	var preferred, rest []uint16
	for _, p := range commonHTTPPorts {
		if portSet[p] {
			preferred = append(preferred, p)
		}
	}

	for _, p := range ports {
		if !portSet[p] {
			continue
		}
		isCommon := slices.Contains(commonHTTPPorts, p)
		if !isCommon {
			rest = append(rest, p)
		}
	}

	return append(preferred, rest...)
}

func ProbeHTTPPort(host string, ports []uint16, timeout time.Duration) (uint16, error) {
	if len(ports) == 0 {
		return 0, fmt.Errorf("no ports to probe")
	}

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: timeout,
			}).DialContext,
			TLSClientConfig:       skipTLSVerify,
			DisableKeepAlives:     true,
			MaxIdleConns:          1,
			IdleConnTimeout:       timeout,
			TLSHandshakeTimeout:   timeout,
			ResponseHeaderTimeout: timeout,
		},
	}

	sorted := preferPorts(ports)

	for _, port := range sorted {
		url := fmt.Sprintf("http://%s:%d/", host, port)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", probeUserAgent)

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()

		if resp.StatusCode > 0 {
			return port, nil
		}
	}

	return 0, fmt.Errorf("no HTTP port found")
}
