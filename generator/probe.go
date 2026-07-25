package generator

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

const probeUserAgent = "DevLocal-Server-Detection"

func ProbeHTTPPort(ports []uint16, timeout time.Duration) (uint16, error) {
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

	for _, port := range ports {
		url := fmt.Sprintf("http://127.0.0.1:%d/", port)
		req, err := http.NewRequest("GET", url, nil)
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
