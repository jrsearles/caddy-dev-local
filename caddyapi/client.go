package caddyapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL    = "http://localhost:2019"
	defaultServerName = "srv0"
	ownedPrefix       = "devlocal-"
	tlsPolicyID       = "devlocal-tls"
)

type Options struct {
	BaseURL           string
	ServerName        string
	AllowCreateServer bool
	HTTPClient        *http.Client
	HTTPPort          int
	HTTPSPort         int
}

type Client struct {
	baseURL           string
	serverName        string
	allowCreateServer bool
	httpClient        *http.Client
	httpPort          int
	httpsPort         int
	mu                sync.Mutex
}

func New(options Options) *Client {
	if options.BaseURL == "" {
		options.BaseURL = defaultBaseURL
	}
	if options.ServerName == "" {
		options.ServerName = defaultServerName
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{
			Transport: &http.Transport{DisableKeepAlives: true},
			Timeout:   30 * time.Second,
		}
	}
	if options.HTTPPort == 0 {
		options.HTTPPort = 80
	}
	if options.HTTPSPort == 0 {
		options.HTTPSPort = 443
	}
	return &Client{
		baseURL:           strings.TrimRight(options.BaseURL, "/"),
		serverName:        options.ServerName,
		allowCreateServer: options.AllowCreateServer,
		httpClient:        options.HTTPClient,
		httpPort:          options.HTTPPort,
		httpsPort:         options.HTTPSPort,
	}
}

func (c *Client) RunningConfig(ctx context.Context) (string, error) {
	status, body, err := c.request(ctx, http.MethodGet, "/config/", nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", responseError(http.MethodGet, "/config/", status, body)
	}
	return string(body), nil
}

func (c *Client) Reconcile(ctx context.Context, desiredRoutes, desiredPolicies map[string]json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	routesPath := "/config/apps/http/servers/" + url.PathEscape(c.serverName) + "/routes"
	actualRoutes, err := c.getArray(ctx, routesPath)
	if err != nil {
		var missing *missingResourceError
		if !asMissing(err, &missing) {
			return fmt.Errorf("reading target server routes: %w", err)
		}
		if !c.allowCreateServer {
			return fmt.Errorf("target Caddy HTTP server %q is absent and server creation is disabled: %w", c.serverName, err)
		}
		if ensureErr := c.ensureServerAndRoutes(ctx, routesPath); ensureErr != nil {
			return ensureErr
		}
		actualRoutes, err = c.getArray(ctx, routesPath)
		if err != nil {
			return fmt.Errorf("reading routes after creating target server: %w", err)
		}
	}

	if err := c.reconcileRoutes(ctx, routesPath, actualRoutes, desiredRoutes); err != nil {
		return err
	}
	if err := c.reconcilePolicies(ctx, desiredPolicies); err != nil {
		return err
	}
	return nil
}

func (c *Client) Cleanup(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var cleanupErrors []error
	routesPath := "/config/apps/http/servers/" + url.PathEscape(c.serverName) + "/routes"
	actualRoutes, err := c.getArray(ctx, routesPath)
	if err == nil {
		if reconcileErr := c.reconcileRoutes(ctx, routesPath, actualRoutes, nil); reconcileErr != nil {
			cleanupErrors = append(cleanupErrors, reconcileErr)
		}
	} else {
		var missing *missingResourceError
		if !asMissing(err, &missing) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("reading target server routes: %w", err))
		}
	}

	if err := c.cleanupPolicies(ctx); err != nil {
		cleanupErrors = append(cleanupErrors, err)
	}
	return errors.Join(cleanupErrors...)
}

func (c *Client) cleanupPolicies(ctx context.Context) error {
	const path = "/config/apps/tls/automation/policies"
	actual, err := c.getArray(ctx, path)
	if err != nil {
		var missing *missingResourceError
		if asMissing(err, &missing) {
			return nil
		}
		return fmt.Errorf("reading TLS policies: %w", err)
	}
	remaining := make([]json.RawMessage, 0, len(actual))
	for _, policy := range actual {
		id, _ := resourceID(policy)
		if id != tlsPolicyID {
			remaining = append(remaining, policy)
		}
	}
	body, err := json.Marshal(remaining)
	if err != nil {
		return fmt.Errorf("encoding TLS policies: %w", err)
	}
	actualBody, err := json.Marshal(actual)
	if err != nil {
		return fmt.Errorf("encoding existing TLS policies: %w", err)
	}
	if jsonEqual(actualBody, body) {
		return nil
	}
	if err := c.change(ctx, http.MethodPatch, path, body); err != nil {
		return fmt.Errorf("cleaning TLS policies: %w", err)
	}
	return nil
}

func (c *Client) ensureServerAndRoutes(ctx context.Context, routesPath string) error {
	serverPath := "/config/apps/http/servers/" + url.PathEscape(c.serverName)
	status, response, err := c.request(ctx, http.MethodGet, serverPath, nil)
	if err != nil {
		return fmt.Errorf("checking target server: %w", err)
	}
	if isMissingResource(status, response) {
		return c.createServer(ctx)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("checking target server: %w", responseError(http.MethodGet, serverPath, status, response))
	}
	status, response, err = c.request(ctx, http.MethodPut, routesPath, []byte(`[]`))
	if err != nil {
		return fmt.Errorf("creating target server routes: %w", err)
	}
	if status != http.StatusConflict && (status < 200 || status >= 300) {
		return fmt.Errorf("creating target server routes: %w", responseError(http.MethodPut, routesPath, status, response))
	}
	return nil
}

type missingResourceError struct {
	method string
	path   string
	status int
	body   []byte
}

func (e *missingResourceError) Error() string {
	return responseError(e.method, e.path, e.status, e.body).Error()
}

func asMissing(err error, target **missingResourceError) bool {
	e, ok := err.(*missingResourceError)
	if ok {
		*target = e
	}
	return ok
}

func (c *Client) getArray(ctx context.Context, path string) ([]json.RawMessage, error) {
	status, body, err := c.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if isMissingResource(status, body) {
		return nil, &missingResourceError{method: http.MethodGet, path: path, status: status, body: body}
	}
	if status < 200 || status >= 300 {
		return nil, responseError(http.MethodGet, path, status, body)
	}
	var result []json.RawMessage
	if len(body) != 0 && string(body) != "null" {
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("decoding %s: %w", path, err)
		}
	}
	return result, nil
}

func (c *Client) createServer(ctx context.Context) error {
	httpPort, httpsPort, err := c.effectivePorts(ctx)
	if err != nil {
		return err
	}
	path := "/config/apps/http/servers/" + url.PathEscape(c.serverName)
	body, err := json.Marshal(map[string]any{
		"listen": []string{
			net.JoinHostPort("", strconv.Itoa(httpsPort)),
			net.JoinHostPort("", strconv.Itoa(httpPort)),
		},
		"routes": []any{},
	})
	if err != nil {
		return fmt.Errorf("encoding target server: %w", err)
	}
	status, response, err := c.request(ctx, http.MethodPut, path, body)
	if err != nil {
		return fmt.Errorf("creating target server: %w", err)
	}
	if status == http.StatusConflict {
		return nil
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("creating target server: %w", responseError(http.MethodPut, path, status, response))
	}
	return nil
}

func (c *Client) effectivePorts(ctx context.Context) (int, int, error) {
	status, body, err := c.request(ctx, http.MethodGet, "/config/apps/http", nil)
	if err != nil {
		return 0, 0, fmt.Errorf("reading effective HTTP ports: %w", err)
	}
	if isMissingResource(status, body) {
		return c.httpPort, c.httpsPort, nil
	}
	if status < 200 || status >= 300 {
		return 0, 0, responseError(http.MethodGet, "/config/apps/http", status, body)
	}
	var app struct {
		HTTPPort  int `json:"http_port"`
		HTTPSPort int `json:"https_port"`
	}
	if err := json.Unmarshal(body, &app); err != nil {
		return 0, 0, fmt.Errorf("decoding effective HTTP ports: %w", err)
	}
	if app.HTTPPort == 0 {
		app.HTTPPort = c.httpPort
	}
	if app.HTTPSPort == 0 {
		app.HTTPSPort = c.httpsPort
	}
	return app.HTTPPort, app.HTTPSPort, nil
}

func (c *Client) reconcileRoutes(ctx context.Context, routesPath string, actual []json.RawMessage, desired map[string]json.RawMessage) error {
	actualOwned := resourcesByID(actual, func(id string) bool { return strings.HasPrefix(id, ownedPrefix) })
	for _, id := range sortedKeys(desired) {
		resource := desired[id]
		if actualResource, exists := actualOwned[id]; exists {
			if jsonEqual(actualResource, resource) {
				continue
			}
			path := "/id/" + url.PathEscape(id)
			status, response, err := c.request(ctx, http.MethodPatch, path, resource)
			if err != nil {
				return fmt.Errorf("updating route %s: %w", id, err)
			}
			if status == http.StatusNotFound {
				if err := c.change(ctx, http.MethodPost, routesPath+"/-", resource); err != nil {
					return fmt.Errorf("re-adding route %s: %w", id, err)
				}
			} else if status < 200 || status >= 300 {
				return fmt.Errorf("updating route %s: %w", id, responseError(http.MethodPatch, path, status, response))
			}
			continue
		}
		if err := c.change(ctx, http.MethodPost, routesPath+"/-", resource); err != nil {
			return fmt.Errorf("adding route %s: %w", id, err)
		}
	}
	for _, id := range sortedKeys(actualOwned) {
		if _, keep := desired[id]; keep {
			continue
		}
		status, response, err := c.request(ctx, http.MethodDelete, "/id/"+url.PathEscape(id), nil)
		if err != nil {
			return fmt.Errorf("deleting orphan route %s: %w", id, err)
		}
		if status != http.StatusNotFound && (status < 200 || status >= 300) {
			return fmt.Errorf("deleting orphan route %s: %w", id, responseError(http.MethodDelete, "/id/"+url.PathEscape(id), status, response))
		}
	}
	return nil
}

func (c *Client) reconcilePolicies(ctx context.Context, desired map[string]json.RawMessage) error {
	const path = "/config/apps/tls/automation/policies"
	actual, err := c.getArray(ctx, path)
	missing := false
	if err != nil {
		var missingErr *missingResourceError
		if !asMissing(err, &missingErr) {
			return fmt.Errorf("reading TLS policies: %w", err)
		}
		missing = true
	}

	merged := make([]json.RawMessage, 0, len(actual)+len(desired))
	if policy, ok := desired[tlsPolicyID]; ok {
		merged = append(merged, policy)
	}
	for _, id := range sortedKeys(desired) {
		if id != tlsPolicyID {
			merged = append(merged, desired[id])
		}
	}
	for _, policy := range actual {
		id, _ := resourceID(policy)
		if id == tlsPolicyID {
			continue
		}
		merged = append(merged, policy)
	}
	body, err := json.Marshal(merged)
	if err != nil {
		return fmt.Errorf("encoding TLS policies: %w", err)
	}
	actualBody, err := json.Marshal(actual)
	if err != nil {
		return fmt.Errorf("encoding existing TLS policies: %w", err)
	}
	if !missing && jsonEqual(actualBody, body) {
		return nil
	}
	method := http.MethodPatch
	if missing {
		method = http.MethodPut
	}
	if err := c.change(ctx, method, path, body); err != nil {
		return fmt.Errorf("reconciling TLS policies: %w", err)
	}
	return nil
}

func jsonEqual(a, b []byte) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return bytes.Equal(a, b)
	}
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}

func resourcesByID(resources []json.RawMessage, owned func(string) bool) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage)
	for _, resource := range resources {
		if id, ok := resourceID(resource); ok && owned(id) {
			result[id] = resource
		}
	}
	return result
}

func resourceID(resource json.RawMessage) (string, bool) {
	var value struct {
		ID string `json:"@id"`
	}
	if json.Unmarshal(resource, &value) != nil || value.ID == "" {
		return "", false
	}
	return value.ID, true
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func (c *Client) change(ctx context.Context, method, path string, body []byte) error {
	status, response, err := c.request(ctx, method, path, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return responseError(method, path, status, response)
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, responseBody, nil
}

func responseError(method, path string, status int, body []byte) error {
	text := strings.TrimSpace(string(body))
	if text == "" {
		text = http.StatusText(status)
	}
	return fmt.Errorf("%s %s returned %d: %s", method, path, status, text)
}

func isMissingResource(status int, body []byte) bool {
	return status == http.StatusNotFound ||
		(status == http.StatusBadRequest && bytes.Contains(body, []byte("invalid traversal path")))
}
