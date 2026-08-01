package caddydevlocal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"
)

type adminAPI struct {
	mu           sync.Mutex
	baseURL      string
	client       *http.Client
	prevRoutes   map[string]json.RawMessage
	prevPolicies map[string]json.RawMessage
}

func newAdminAPI() *adminAPI {
	return &adminAPI{
		baseURL: "http://localhost:2019",
		client: &http.Client{
			Transport: &http.Transport{DisableKeepAlives: true},
			Timeout:   30 * time.Second,
		},
	}
}

func (a *adminAPI) request(method, path, contentType string, body []byte) (int, error) {
	var (
		status int
		err    error
	)
	for attempt := 0; attempt < 3; attempt++ {
		status, _, err = a.rawRequest(method, path, contentType, body)
		if err == nil {
			return status, nil
		}
		if method != http.MethodGet && method != http.MethodPut && method != http.MethodPatch && method != http.MethodDelete {
			return status, err
		}
		if !isTransientNetError(err) {
			return status, err
		}
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}
	return status, err
}

func (a *adminAPI) getConfig(path string) (int, []byte, error) {
	var (
		status int
		body   []byte
		err    error
	)
	for attempt := 0; attempt < 3; attempt++ {
		status, body, err = a.rawRequest(http.MethodGet, path, "", nil)
		if err == nil {
			return status, body, nil
		}
		if !isTransientNetError(err) {
			return status, nil, err
		}
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}
	return status, body, err
}

func (a *adminAPI) rawRequest(method, path, contentType string, body []byte) (int, []byte, error) {
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(method, a.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Content-Type", contentType)
	} else {
		req, err = http.NewRequest(method, a.baseURL+path, nil)
		if err != nil {
			return 0, nil, err
		}
	}
	resp, err := a.client.Do(req) //nolint:gosec // paths are constants, never user-controlled
	if err != nil {
		return 0, nil, err
	}
	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, respBody, nil
}

func isTransientNetError(err error) bool {
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func (a *adminAPI) doDelete(path string) error {
	status, err := a.request(http.MethodDelete, path, "", nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent && status != http.StatusNotFound {
		return fmt.Errorf("DELETE %s: %s", path, http.StatusText(status))
	}
	return nil
}

func (a *adminAPI) doPost(path, contentType string, body []byte) error {
	status, err := a.request(http.MethodPost, path, contentType, body)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("POST %s: %s", path, http.StatusText(status))
	}
	return nil
}

func (a *adminAPI) doPut(path string, body []byte) (int, error) {
	return a.request(http.MethodPut, path, "application/json", body)
}

func (a *adminAPI) doPatch(path string, body []byte) (int, error) {
	return a.request(http.MethodPatch, path, "application/json", body)
}

func (a *adminAPI) deleteByID(id string) error {
	return a.doDelete("/id/" + id)
}

func (a *adminAPI) patchByID(id string, obj json.RawMessage) (int, error) {
	return a.doPatch("/id/"+id, obj)
}

func (a *adminAPI) postRoute(obj json.RawMessage) error {
	return a.doPost("/config/apps/http/servers/"+devlocalServerName+"/routes/-", "application/json", obj)
}

func (a *adminAPI) getStatus(path string) (int, error) {
	return a.request(http.MethodGet, path, "", nil)
}

func (a *adminAPI) effectivePorts() (httpPort, httpsPort int, err error) {
	httpPort, httpsPort = 80, 443
	status, body, err := a.getConfig("/config/apps/http")
	if err != nil {
		return 0, 0, err
	}
	if status == http.StatusOK {
		var httpApp struct {
			HTTPPort  int `json:"http_port"`
			HTTPSPort int `json:"https_port"`
		}
		if err := json.Unmarshal(body, &httpApp); err == nil {
			if httpApp.HTTPPort != 0 {
				httpPort = httpApp.HTTPPort
			}
			if httpApp.HTTPSPort != 0 {
				httpsPort = httpApp.HTTPSPort
			}
		}
	}
	return httpPort, httpsPort, nil
}

func (a *adminAPI) ensureServer() error {
	status, err := a.getStatus("/config/apps/http/servers/" + devlocalServerName)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		httpPort, httpsPort, pErr := a.effectivePorts()
		if pErr != nil {
			return pErr
		}
		listenAny := []any{
			net.JoinHostPort("", strconv.Itoa(httpsPort)),
			net.JoinHostPort("", strconv.Itoa(httpPort)),
		}
		serverBody, mErr := json.Marshal(map[string]any{"listen": listenAny, "routes": []any{}})
		if mErr != nil {
			return mErr
		}
		status, err = a.doPut("/config/apps/http/servers/"+devlocalServerName, serverBody)
		if err != nil {
			return err
		}
		if status != http.StatusConflict && (status < 200 || status > 299) {
			return fmt.Errorf("ensuring server: status %d", status)
		}
	}

	status, err = a.getStatus("/config/apps/http/servers/" + devlocalServerName + "/routes")
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		status, err = a.doPut("/config/apps/http/servers/"+devlocalServerName+"/routes", []byte(`[]`))
		if err != nil {
			return err
		}
		if status != http.StatusConflict && (status < 200 || status > 299) {
			return fmt.Errorf("ensuring routes: status %d", status)
		}
	}

	status, err = a.getStatus("/config/apps/tls/automation/policies")
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		status, err = a.doPut("/config/apps/tls/automation/policies", []byte(`[]`))
		if err != nil {
			return err
		}
		if status != http.StatusConflict && (status < 200 || status > 299) {
			return fmt.Errorf("ensuring TLS policies: status %d", status)
		}
	}
	return nil
}

func (a *adminAPI) prependTLSPolicy(policy json.RawMessage) error {
	status, body, err := a.getConfig("/config/apps/tls/automation/policies")
	if err != nil {
		return err
	}
	var policies []json.RawMessage
	exists := status == http.StatusOK
	if exists && len(body) > 0 {
		if uErr := json.Unmarshal(body, &policies); uErr != nil {
			return fmt.Errorf("decoding existing TLS policies: %w", uErr)
		}
	}
	merged := make([]json.RawMessage, 0, len(policies)+1)
	merged = append(merged, policy)
	for _, p := range policies {
		var obj map[string]any
		if uErr := json.Unmarshal(p, &obj); uErr == nil {
			if id, _ := obj["@id"].(string); id == devlocalTLSPolicyID {
				continue
			}
		}
		merged = append(merged, p)
	}
	result, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	if exists {
		status, err = a.doPatch("/config/apps/tls/automation/policies", result)
	} else {
		status, err = a.doPut("/config/apps/tls/automation/policies", result)
	}
	if err != nil {
		return err
	}
	if status != http.StatusNotFound && status != http.StatusConflict && (status < 200 || status > 299) {
		return fmt.Errorf("prepending TLS policy: status %d", status)
	}
	if status == http.StatusNotFound {
		status, err = a.doPut("/config/apps/tls/automation/policies", result)
		if err != nil {
			return err
		}
		if status != http.StatusConflict && (status < 200 || status > 299) {
			return fmt.Errorf("prepending TLS policy: status %d", status)
		}
	}
	return nil
}

func (a *adminAPI) reconcileDevlocal(routes, policies map[string]json.RawMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(routes) > 0 {
		if err := a.ensureServer(); err != nil {
			return fmt.Errorf("ensuring server: %w", err)
		}
	}

	removedRoutes, updatedRoutes, addedRoutes := computePatch(a.prevRoutes, routes)
	for id := range removedRoutes {
		if err := a.deleteByID(id); err != nil {
			return fmt.Errorf("deleting route %s: %w", id, err)
		}
	}
	for _, id := range sortedMapKeys(updatedRoutes) {
		status, err := a.patchByID(id, updatedRoutes[id])
		if err != nil {
			return fmt.Errorf("updating route %s: %w", id, err)
		}
		if status == http.StatusNotFound {
			if err := a.postRoute(updatedRoutes[id]); err != nil {
				return fmt.Errorf("re-adding route %s: %w", id, err)
			}
		} else if status < 200 || status > 299 {
			return fmt.Errorf("updating route %s: status %d", id, status)
		}
	}
	for _, id := range sortedMapKeys(addedRoutes) {
		if err := a.postRoute(addedRoutes[id]); err != nil {
			return fmt.Errorf("adding route %s: %w", id, err)
		}
	}

	removedPolicies, updatedPolicies, addedPolicies := computePatch(a.prevPolicies, policies)
	for id := range removedPolicies {
		if err := a.deleteByID(id); err != nil {
			return fmt.Errorf("deleting TLS policy %s: %w", id, err)
		}
	}
	for _, id := range sortedMapKeys(updatedPolicies) {
		status, err := a.patchByID(id, updatedPolicies[id])
		if err != nil {
			return fmt.Errorf("updating TLS policy %s: %w", id, err)
		}
		if status == http.StatusNotFound {
			if err := a.prependTLSPolicy(updatedPolicies[id]); err != nil {
				return fmt.Errorf("re-adding TLS policy %s: %w", id, err)
			}
		} else if status < 200 || status > 299 {
			return fmt.Errorf("updating TLS policy %s: status %d", id, status)
		}
	}
	for _, id := range sortedMapKeys(addedPolicies) {
		if err := a.prependTLSPolicy(addedPolicies[id]); err != nil {
			return fmt.Errorf("adding TLS policy %s: %w", id, err)
		}
	}

	a.prevRoutes = routes
	a.prevPolicies = policies
	return nil
}

func computePatch(prev, desired map[string]json.RawMessage) (removed, updated, added map[string]json.RawMessage) {
	removed = make(map[string]json.RawMessage)
	updated = make(map[string]json.RawMessage)
	added = make(map[string]json.RawMessage)

	for id, obj := range prev {
		desiredObj, ok := desired[id]
		if !ok {
			removed[id] = obj
		} else if !bytes.Equal(obj, desiredObj) {
			updated[id] = desiredObj
		}
	}
	for id, obj := range desired {
		if _, ok := prev[id]; !ok {
			added[id] = obj
		}
	}
	return removed, updated, added
}

func sortedMapKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
