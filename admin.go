package caddydevlocal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type adminAPI struct {
	baseURL       string
	client        *http.Client
	prevRouteIDs  []string
	prevPolicyIDs []string
}

func newAdminAPI() *adminAPI {
	return &adminAPI{
		baseURL: "http://localhost:2019",
		client:  &http.Client{},
	}
}

func (a *adminAPI) doDelete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, a.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("DELETE %s: %s", path, resp.Status)
	}
	return nil
}

func (a *adminAPI) doPost(path, contentType string, body []byte) error {
	resp, err := a.client.Post(a.baseURL+path, contentType, bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("POST %s: %s", path, resp.Status)
	}
	return nil
}

func (a *adminAPI) loadConfig(config []byte, contentType string) error {
	return a.doPost("/load", contentType, config)
}

func (a *adminAPI) deleteByID(id string) error {
	return a.doDelete("/id/" + id)
}

func (a *adminAPI) clearDevlocal() {
	for _, id := range a.prevRouteIDs {
		a.deleteByID(id) //nolint:errcheck
	}
	for _, id := range a.prevPolicyIDs {
		a.deleteByID(id) //nolint:errcheck
	}
	a.prevRouteIDs = nil
	a.prevPolicyIDs = nil
}

func (a *adminAPI) postRoute(serverName string, route json.RawMessage) error {
	return a.doPost("/config/apps/http/servers/"+serverName+"/routes/-", "application/json", route)
}

func (a *adminAPI) postTLSPolicy(policy json.RawMessage) error {
	return a.doPost("/config/apps/tls/automation/policies/-", "application/json", policy)
}

func (a *adminAPI) ensureServer() error {
	req, err := http.NewRequest(http.MethodGet, a.baseURL+"/config/apps/http/servers/srv0", nil)
	if err != nil {
		return err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return a.doPost("/config/apps/http/servers/srv0", "application/json", []byte(`{"listen":[":443",":80"]}`))
	}
	return nil
}

func (a *adminAPI) syncDevlocal(configJSON json.RawMessage) error {
	a.clearDevlocal()

	var config map[string]any
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return err
	}

	apps, _ := config["apps"].(map[string]any)
	if apps == nil {
		return fmt.Errorf("config missing apps")
	}

	var newRouteIDs []string
	var newPolicyIDs []string

	if httpApps, ok := apps["http"].(map[string]any); ok {
		if servers, ok := httpApps["servers"].(map[string]any); ok {
			for srvName, srv := range servers {
				if srvMap, ok := srv.(map[string]any); ok {
					if routes, ok := srvMap["routes"].([]any); ok {
						for _, r := range routes {
							if route, ok := r.(map[string]any); ok {
								if id, ok := route["@id"].(string); ok {
									newRouteIDs = append(newRouteIDs, id)
								}
								routeJSON, err := json.Marshal(r)
								if err != nil {
									return err
								}
								if err := a.postRoute(srvName, routeJSON); err != nil {
									return fmt.Errorf("posting route to %s: %w", srvName, err)
								}
							}
						}
					}
				}
			}
		}
	}

	if tls, ok := apps["tls"].(map[string]any); ok {
		if automation, ok := tls["automation"].(map[string]any); ok {
			if policies, ok := automation["policies"].([]any); ok {
				for _, p := range policies {
					if policy, ok := p.(map[string]any); ok {
						if id, ok := policy["@id"].(string); ok {
							newPolicyIDs = append(newPolicyIDs, id)
						}
						policyJSON, err := json.Marshal(p)
						if err != nil {
							return err
						}
						if err := a.postTLSPolicy(policyJSON); err != nil {
							return fmt.Errorf("posting TLS policy: %w", err)
						}
					}
				}
			}
		}
	}

	a.prevRouteIDs = newRouteIDs
	a.prevPolicyIDs = newPolicyIDs
	return nil
}
