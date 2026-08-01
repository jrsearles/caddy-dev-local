package caddydevlocal

import (
	"encoding/json"
	"reflect"
	"testing"

	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/standard"
)

func TestParseCaddyfileListenPorts(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		wantHTTP  int
		wantHTTPS int
		wantOK    bool
		wantErr   bool
	}{
		{"empty", "", 0, 0, false, false},
		{"comments only", "# just a comment\n", 0, 0, false, false},
		{"globals only", "{\n\thttp_port 8080\n\thttps_port 8443\n}\n", 8080, 8443, true, false},
		{"http only", "{\n\thttp_port 8080\n}\n", 8080, 0, true, false},
		{"https only", "{\n\thttps_port 8443\n}\n", 0, 8443, true, false},
		{"site block only", "example.com {\n\treverse_proxy localhost:8080\n}\n", 0, 0, false, false},
		{"globals and site", "{\n\thttp_port 8080\n\thttps_port 8443\n}\nexample.com {\n\treverse_proxy localhost:8080\n}\n", 8080, 8443, true, false},
		{"invalid port", "{\n\thttp_port notaport\n}\n", 0, 0, false, true},
		{"missing argument", "{\n\thttp_port\n}\n", 0, 0, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpPort, httpsPort, ok, err := parseCaddyfileListenPorts([]byte(tt.source))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if httpPort != tt.wantHTTP {
				t.Errorf("http_port = %d, want %d", httpPort, tt.wantHTTP)
			}
			if httpsPort != tt.wantHTTPS {
				t.Errorf("https_port = %d, want %d", httpsPort, tt.wantHTTPS)
			}
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
		})
	}
}

func TestAdaptUserConfig(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		adapterName string
		wantJSON    string
	}{
		{
			"empty caddyfile",
			"",
			adapterCaddyfile,
			`{}`,
		},
		{
			"comments only",
			"# just a comment\n",
			adapterCaddyfile,
			`{}`,
		},
		{
			"globals only",
			"{\n\temail dev@example.com\n\tdebug\n}\n",
			adapterCaddyfile,
			`{"logging":{"logs":{"default":{"level":"DEBUG"}}}}`,
		},
		{
			"globals ports only",
			"{\n\thttp_port 8080\n\thttps_port 8443\n}\n",
			adapterCaddyfile,
			`{}`,
		},
		{
			"json passthrough",
			`{"apps":{"http":{"servers":{"srv0":{"listen":[":443"],"routes":[]}}}}}`,
			"json",
			`{"apps":{"http":{"servers":{"srv0":{"listen":[":443"],"routes":[]}}}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := adaptUserConfig([]byte(tt.source), tt.adapterName)
			if err != nil {
				t.Fatal(err)
			}

			var gotAny, wantAny any
			if err := json.Unmarshal(result, &gotAny); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tt.wantJSON), &wantAny); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotAny, wantAny) {
				t.Errorf("result = %s, want %s", result, tt.wantJSON)
			}
		})
	}
}

func TestAdaptUserConfigKeepsSiteBlocks(t *testing.T) {
	result, err := adaptUserConfig([]byte("{\n\temail dev@example.com\n}\nexample.com {\n\trespond \"hi\"\n}\n"), adapterCaddyfile)
	if err != nil {
		t.Fatal(err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(result, &cfg); err != nil {
		t.Fatal(err)
	}

	apps, ok := cfg["apps"].(map[string]any)
	if !ok {
		t.Fatalf("apps missing from adapted config: %s", result)
	}
	httpApp, ok := apps["http"].(map[string]any)
	if !ok {
		t.Fatalf("http app missing from adapted config (site block was stripped): %s", result)
	}
	servers, ok := httpApp["servers"].(map[string]any)
	if !ok || len(servers) == 0 {
		t.Fatalf("no http servers in adapted config (site block was stripped): %s", result)
	}
	srv, ok := servers["srv0"].(map[string]any)
	if !ok {
		t.Fatalf("srv0 missing from adapted config: %s", result)
	}
	routes, ok := srv["routes"].([]any)
	if !ok || len(routes) == 0 {
		t.Fatalf("no routes on srv0: %s", result)
	}

	tlsApp, ok := apps["tls"].(map[string]any)
	if !ok {
		t.Fatalf("tls app missing from adapted config: %s", result)
	}
	automation := tlsApp["automation"].(map[string]any)
	policies := automation["policies"].([]any)
	subjects := policies[0].(map[string]any)["subjects"].([]any)
	if len(subjects) != 1 || subjects[0] != "example.com" {
		t.Errorf("tls policy subjects = %v, want [example.com]", subjects)
	}
}

func TestFingerprintDomains(t *testing.T) {
	a := map[string][]string{
		"b.dev.local": {"127.0.0.1:8080", "127.0.0.1:8081"},
		"a.dev.local": {"127.0.0.1:8082"},
	}
	b := map[string][]string{
		"a.dev.local": {"127.0.0.1:8082"},
		"b.dev.local": {"127.0.0.1:8081", "127.0.0.1:8080"},
	}

	if fingerprintDomains(a) != fingerprintDomains(b) {
		t.Error("fingerprint must be order-independent for both domains and targets")
	}

	c := map[string][]string{
		"a.dev.local": {"127.0.0.1:8082"},
		"b.dev.local": {"127.0.0.1:8080"},
	}
	if fingerprintDomains(a) == fingerprintDomains(c) {
		t.Error("fingerprints must differ when targets differ")
	}

	d := map[string][]string{
		"a.dev.local": {"127.0.0.1:8082"},
	}
	if fingerprintDomains(b) == fingerprintDomains(d) {
		t.Error("fingerprints must differ when a domain is removed")
	}

	if fingerprintDomains(nil) != fingerprintDomains(map[string][]string{}) {
		t.Error("nil and empty maps must fingerprint identically")
	}
}

func TestInjectListenPorts(t *testing.T) {
	tests := []struct {
		name          string
		userJSON      string
		httpPort      int
		httpsPort     int
		wantJSON      string
		wantUnchanged bool
	}{
		{
			"empty config gets ports",
			`{}`,
			80, 443,
			`{"apps":{"http":{"http_port":80,"https_port":443}}}`,
			false,
		},
		{
			"custom ports injected",
			`{}`,
			8080, 8443,
			`{"apps":{"http":{"http_port":8080,"https_port":8443}}}`,
			false,
		},
		{
			"config with servers unchanged",
			`{"apps":{"http":{"servers":{"srv0":{"listen":[":443"],"routes":[]}}}}}`,
			80, 443,
			`{"apps":{"http":{"servers":{"srv0":{"listen":[":443"],"routes":[]}}}}}`,
			true,
		},
		{
			"existing ports preserved",
			`{"apps":{"http":{"http_port":9090}}}`,
			80, 443,
			`{"apps":{"http":{"http_port":9090,"https_port":443}}}`,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := injectListenPorts([]byte(tt.userJSON), tt.httpPort, tt.httpsPort)
			if err != nil {
				t.Fatal(err)
			}
			var gotAny, wantAny any
			if err := json.Unmarshal(result, &gotAny); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tt.wantJSON), &wantAny); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotAny, wantAny) {
				t.Errorf("result = %s, want %s", result, tt.wantJSON)
			}
		})
	}
}
