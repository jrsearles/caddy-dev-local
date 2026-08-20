package generator

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestRenderSanity(t *testing.T) {
	now := time.Now()
	containers := []*ContainerInfo{
		{
			ContainerName: "my-nginx",
			ContainerID:   "abcdef123456789",
			Image:         "nginx:latest",
			Ports:         []uint16{80},
			SelectedPort:  80,
			TargetKind:    targetGateway,
			IsRunning:     true,
			Created:       now,
			Networks:      []string{"bridge"},
			Health:        "healthy",
		},
		{
			ContainerName: "web-1",
			ContainerID:   "bbbbbbbbbbbb",
			Image:         "myapp:latest",
			Ports:         []uint16{8080},
			SelectedPort:  8080,
			TargetKind:    targetDNS,
			IsRunning:     true,
			Created:       now,
			IsCompose:     true,
			Project:       "demo",
			Service:       "web",
			Networks:      []string{"demo_default"},
		},
		{
			ContainerName: "db-1",
			ContainerID:   "cccccccccccc",
			Image:         "postgres:16",
			Ports:         []uint16{5432},
			SelectedPort:  5432,
			TargetKind:    targetDNS,
			IsRunning:     false,
			Created:       now,
			LastStopped:   now,
			IsCompose:     true,
			Project:       "demo",
			Service:       "db",
		},
	}

	page := GenerateIndexPage("dev.local", false, containers, "", "", 0)

	checks := []string{
		`<title>devlocal — Container Index</title>`,
		`fonts.googleapis.com`,
		`class="card"`,
		`class="container-icon"`,
		`data-name="my-nginx"`,
		`data-domains=`,
		`data-id="abcdef123456"`,
		`data-networks="bridge"`,
		`data-health="healthy"`,
		`class="health-badge health-healthy"`,
		`docker-desktop://dashboard/open`,
		`docker-desktop://dashboard/apps/demo`,
		`Open Docker Desktop`,
		`Open demo in Docker Desktop`,
		`data-project="demo"`,
		`class="project-section"`,
		`class="project-toggle"`,
		`class="project-cards"`,
		`class="mini-chip mini-chip-running"`,
		`class="mini-chip mini-chip-stopped"`,
		`aria-expanded="false"`,
		`id="liveDot"`,
		`status-running`,
		`status-stopped`,
		`data-copy=`,
		`sessionStorage.setItem('devlocal-open'`,
		`fetch('/version.json'`,
		`id="themeToggle"`,
		`onclick="cycleTheme()"`,
		`localStorage.getItem('devlocal-theme')`,
		`<link rel="stylesheet" href="index.css">`,
		`>2 running<`,
		`>1 stopped<`,
		`>1 project<`,
		`id="drawer"`,
		`id="drawerBody"`,
		`function openDrawer`,
		`function closeDrawer`,
		`id="devlocal-search"`,
		`function runFilter`,
		`function pushState`,
		`history.replaceState`,
		`sessionStorage.setItem('devlocal-scroll'`,
		`window.scrollTo`,
	}
	for _, c := range checks {
		if !strings.Contains(page, c) {
			t.Errorf("page missing %q", c)
		}
	}

	// banner should not appear when DiscoveryError is empty
	if strings.Contains(page, `id="errorBanner"`) {
		t.Error("error banner rendered with no discovery error")
	}

	if strings.Contains(page, `id="tab-config"`) {
		t.Error("config tab rendered with empty config")
	}
	if strings.Contains(page, `id="config-json"`) {
		t.Error("config JSON script rendered with empty config")
	}
	if !strings.Contains(page, `class="project-header"`) {
		t.Error("project header wrapper missing")
	}
	if strings.Contains(page, "rowspan") {
		t.Error("rowspan table logic still present")
	}
	if strings.Contains(page, "displayRow") {
		t.Error("displayRow still referenced")
	}
	if got := strings.Count(page, `class="dd-link"`); got != 2 {
		t.Errorf("expected 2 Docker Desktop links (standalone card + project header), got %d", got)
	}
}

func TestRenderDiscoveryErrorBanner(t *testing.T) {
	page := GenerateIndexPage("dev.local", false, nil, "", "Docker socket unreachable", 1700000000)

	if !strings.Contains(page, `id="errorBanner"`) {
		t.Error("error banner missing when DiscoveryError is set")
	}
	if !strings.Contains(page, "Docker socket unreachable") {
		t.Error("error message not present in banner")
	}
	if !strings.Contains(page, `data-ts="1700000000"`) {
		t.Error("last refresh timestamp not in banner")
	}
}

func TestRenderDiscoveryErrorInEmptyState(t *testing.T) {
	page := GenerateIndexPage("dev.local", false, nil, "", "connection refused", 0)

	if !strings.Contains(page, "connection refused") {
		t.Error("error message not present in empty state")
	}
	if !strings.Contains(page, "class=\"empty-error\"") {
		t.Error("empty-error block missing")
	}
}

func TestRenderEmptyStateHints(t *testing.T) {
	page := GenerateIndexPage("dev.local", false, nil, "", "", 0)

	hints := []string{
		"No containers registered.",
		"/var/run/docker.sock",
		"publish ports",
		"README",
	}
	for _, h := range hints {
		if !strings.Contains(page, h) {
			t.Errorf("empty state missing hint %q", h)
		}
	}
}

func TestRenderHealthBadge(t *testing.T) {
	now := time.Now()
	containers := []*ContainerInfo{
		{
			ContainerName: "healthy-svc",
			ContainerID:   "aaa",
			Image:         "app:1",
			Ports:         []uint16{80},
			SelectedPort:  80,
			TargetKind:    targetDNS,
			IsRunning:     true,
			Created:       now,
			Health:        "healthy",
		},
		{
			ContainerName: "sick-svc",
			ContainerID:   "bbb",
			Image:         "app:2",
			Ports:         []uint16{80},
			SelectedPort:  80,
			TargetKind:    targetDNS,
			IsRunning:     true,
			Created:       now,
			Health:        "unhealthy",
		},
		{
			ContainerName: "starting-svc",
			ContainerID:   "ccc",
			Image:         "app:3",
			Ports:         []uint16{80},
			SelectedPort:  80,
			TargetKind:    targetDNS,
			IsRunning:     true,
			Created:       now,
			Health:        "starting",
		},
		{
			ContainerName: "no-health-svc",
			ContainerID:   "ddd",
			Image:         "app:4",
			Ports:         []uint16{80},
			SelectedPort:  80,
			TargetKind:    targetDNS,
			IsRunning:     true,
			Created:       now,
			Health:        "",
		},
	}

	page := GenerateIndexPage("dev.local", false, containers, "", "", 0)

	if !strings.Contains(page, `health-badge health-healthy`) {
		t.Error("healthy badge missing")
	}
	if !strings.Contains(page, `health-badge health-unhealthy`) {
		t.Error("unhealthy badge missing")
	}
	if !strings.Contains(page, `health-badge health-starting`) {
		t.Error("starting badge missing")
	}
	if strings.Count(page, `class="health-badge health-healthy"`) != 1 {
		t.Errorf("expected 1 healthy badge")
	}
	if strings.Count(page, `class="health-badge health-unhealthy"`) != 1 {
		t.Errorf("expected 1 unhealthy badge")
	}
	if strings.Count(page, `class="health-badge health-starting"`) != 1 {
		t.Errorf("expected 1 starting badge")
	}
}

func TestRenderHealthBadgeNotShownWhenStopped(t *testing.T) {
	now := time.Now()
	containers := []*ContainerInfo{
		{
			ContainerName: "stopped-svc",
			ContainerID:   "aaa",
			Image:         "app:1",
			Ports:         []uint16{80},
			SelectedPort:  80,
			TargetKind:    targetDNS,
			IsRunning:     false,
			Created:       now,
			LastStopped:   now,
			Health:        "unhealthy",
		},
	}

	page := GenerateIndexPage("dev.local", false, containers, "", "", 0)

	if strings.Contains(page, `class="health-badge health-unhealthy"`) {
		t.Error("health badge should not render for stopped containers")
	}
}

func TestRenderDrawerDataAttrs(t *testing.T) {
	now := time.Now()
	containers := []*ContainerInfo{
		{
			ContainerName:  "myapp",
			ContainerID:    "deadbeef1234",
			Image:          "myapp:latest",
			Ports:          []uint16{8080},
			SelectedPort:   8080,
			TargetKind:     targetDNS,
			IsRunning:      true,
			Created:        now,
			Networks:       []string{"backend", "frontend"},
			Health:         "healthy",
			PublishedPorts: map[uint16]uint16{8080: 32000},
			Labels: map[string]string{
				"dev.local.domains":            "8080:custom.dev.local",
				"com.docker.compose.project":   "proj",
				"com.docker.compose.service":   "app",
				"org.opencontainers.image.url": "https://example.com",
				"unrelated.label":              "ignored",
			},
		},
	}

	page := GenerateIndexPage("dev.local", false, containers, "", "", 0)

	checks := []string{
		`data-id="deadbeef1234"`,
		`data-networks="backend,frontend"`,
		`data-published="8080:32000"`,
		`data-health="healthy"`,
		`data-ip=`,
		`dev.local.domains`,
		`com.docker.compose.project`,
		`org.opencontainers.image.url`,
	}
	for _, c := range checks {
		if !strings.Contains(page, c) {
			t.Errorf("page missing drawer attr/data %q", c)
		}
	}
	if strings.Contains(page, `"unrelated.label"`) {
		t.Error("unrelated label should be filtered from labels JSON")
	}
}

func TestRenderSearchAttrs(t *testing.T) {
	now := time.Now()
	containers := []*ContainerInfo{
		{
			ContainerName: "nginx-proxy",
			ContainerID:   "aaa",
			Image:         "nginx:latest",
			Ports:         []uint16{80},
			SelectedPort:  80,
			TargetKind:    targetGateway,
			IsRunning:     true,
			Created:       now,
			IsCompose:     true,
			Project:       "infra",
			Service:       "proxy",
		},
	}

	page := GenerateIndexPage("dev.local", false, containers, "", "", 0)

	if !strings.Contains(page, `data-domains=`) {
		t.Error("data-domains attribute missing")
	}
	if !strings.Contains(page, `id="devlocal-search"`) {
		t.Error("search input missing")
	}
	if !strings.Contains(page, `function runFilter`) {
		t.Error("runFilter function missing")
	}
	if !strings.Contains(page, `id="noMatch"`) {
		t.Error("no-match element missing")
	}
}

func TestNoExpandCollapseAll(t *testing.T) {
	now := time.Now()
	containers := []*ContainerInfo{
		{
			ContainerName: "web-1",
			ContainerID:   "aaa",
			Image:         "myapp:latest",
			Ports:         []uint16{8080},
			SelectedPort:  8080,
			TargetKind:    targetDNS,
			IsRunning:     true,
			Created:       now,
			IsCompose:     true,
			Project:       "demo",
			Service:       "web",
		},
	}

	page := GenerateIndexPage("dev.local", false, containers, "", "", 0)

	for _, s := range []string{"expandAllProjects", "collapseAllProjects", "Expand all", "Collapse all", "section-btn", "section-actions"} {
		if strings.Contains(page, s) {
			t.Errorf("expand/collapse-all leftover %q", s)
		}
	}
}

func TestRenderConfigToolbarAlignment(t *testing.T) {
	configJSON := `{"apps":{}}`
	page := GenerateIndexPage("dev.local", false, nil, configJSON, "", 0)

	if !strings.Contains(page, `class="config-toolbar"`) {
		t.Error("config toolbar missing")
	}
	if !strings.Contains(page, `class="section-label"`) {
		t.Error("config toolbar section-label missing")
	}
}

func TestRenderVersionPoll(t *testing.T) {
	page := GenerateIndexPage("dev.local", false, nil, "", "", 0)

	if !strings.Contains(page, `fetch('/version.json'`) {
		t.Error("version.json poll missing")
	}
	if !strings.Contains(page, `checkFallback`) {
		t.Error("fallback poll missing")
	}
}

func TestRenderScrollPreserve(t *testing.T) {
	page := GenerateIndexPage("dev.local", false, nil, "", "", 0)

	if !strings.Contains(page, `sessionStorage.setItem('devlocal-scroll'`) {
		t.Error("scroll position save missing")
	}
	if !strings.Contains(page, `window.scrollTo`) {
		t.Error("scroll position restore missing")
	}
}

func TestRenderPortlessContainer(t *testing.T) {
	now := time.Now()
	containers := []*ContainerInfo{
		{
			ContainerName: "no-ports-svc",
			ContainerID:   "porterless123",
			Image:         "redis:7",
			Ports:         nil,
			TargetKind:    targetDNS,
			IsRunning:     true,
			Created:       now,
			Networks:      []string{"backend"},
		},
		{
			ContainerName: "has-ports-svc",
			ContainerID:   "withports1234",
			Image:         "nginx:latest",
			Ports:         []uint16{80},
			SelectedPort:  80,
			TargetKind:    targetDNS,
			IsRunning:     true,
			Created:       now,
			Networks:      []string{"backend"},
		},
	}

	page := GenerateIndexPage("dev.local", false, containers, "", "", 0)

	if !strings.Contains(page, `data-name="no-ports-svc"`) {
		t.Error("portless container card missing")
	}
	if !strings.Contains(page, `>no ports exposed<`) {
		t.Error("no-ports hint missing for portless container")
	}
	if !strings.Contains(page, `data-name="has-ports-svc"`) {
		t.Error("container with ports card missing")
	}
	if strings.Contains(page, `>no ports exposed<`) && strings.Count(page, `>no ports exposed<`) > 1 {
		t.Error("no-ports hint should not appear for containers with ports")
	}
	if strings.Contains(page, `no-ports-svc.dev.local`) {
		t.Error("portless container should not have domain entries")
	}
}

func TestRenderPortlessComposeContainer(t *testing.T) {
	now := time.Now()
	containers := []*ContainerInfo{
		{
			ContainerName: "worker-1",
			ContainerID:   "workerid12345",
			Image:         "myapp:latest",
			Ports:         nil,
			TargetKind:    targetDNS,
			IsRunning:     true,
			Created:       now,
			IsCompose:     true,
			Project:       "myapp",
			Service:       "worker",
			Networks:      []string{"myapp_default"},
		},
	}

	page := GenerateIndexPage("dev.local", false, containers, "", "", 0)

	if !strings.Contains(page, `data-name="worker-1"`) {
		t.Error("portless compose container card missing")
	}
	if !strings.Contains(page, `>no ports exposed<`) {
		t.Error("no-ports hint missing for portless compose container")
	}
	if !strings.Contains(page, `data-project="myapp"`) {
		t.Error("compose project section missing")
	}
}

func TestRenderConfigPanel(t *testing.T) {
	configJSON := `{
  "apps": {
    "http": {
      "servers": {
        "srv0": {
          "routes": [{"match": ["</pre><script>alert(1)</script>"]}]
        }
      }
    }
  }
}`
	page := GenerateIndexPage("dev.local", false, nil, configJSON, "", 0)

	checks := []string{
		`id="tab-config"`,
		`>Caddy<`,
		`class="config-view"`,
		`class="config-toolbar"`,
		`class="section-label"`,
		`class="config-pre"`,
		`id="rawToggle"`,
		`onclick="toggleRawJSON()"`,
		`function toggleRawJSON()`,
		`function switchTab(name)`,
		`&#34;srv0&#34;`,
		`id="config-raw"`,
		`id="config-tree"`,
		`id="config-json"`,
		`id="expandAllBtn"`,
		`id="collapseAllBtn"`,
		`configExpandAll`,
		`configCollapseAll`,
		`renderConfigTree`,
		`@pgrabovets/json-view`,
		`\u003c/script\u003e`,
	}
	for _, c := range checks {
		if !strings.Contains(page, c) {
			t.Errorf("config panel missing %q", c)
		}
	}
	if strings.Contains(page, `class="config-copy-btn"`) {
		t.Error("config panel still has a copy button")
	}
	if strings.Contains(page, `copyConfig`) {
		t.Error("config copy function still present")
	}
	if strings.Contains(page, `</pre><script>alert(1)</script>`) {
		t.Error("config JSON was not HTML-escaped")
	}
	if !strings.Contains(page, `&lt;/pre&gt;&lt;script&gt;alert(1)&lt;/script&gt;`) {
		t.Error("config JSON not escaped properly in pre block")
	}
}

func TestConfigJSONScriptTagParseable(t *testing.T) {
	configJSON := `{"apps":{"http":{"servers":{"srv0":{"routes":[{"@id":"devlocal-route-foo","match":[{"host":["foo.dev.local"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"foo:8080"}]}]}]}}}}}`
	page := GenerateIndexPage("dev.local", false, nil, configJSON, "", 0)

	re := regexp.MustCompile(`<script type="application/json" id="config-json">([\s\S]*?)</script>`)
	m := re.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("config-json script element not found")
	}
	raw := m[1]

	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("config-json content is not a valid JSON object: %v\ncontent: %s", err, raw)
	}
	servers, _ := cfg["apps"].(map[string]any)["http"].(map[string]any)["servers"].(map[string]any)
	srv0, _ := servers["srv0"].(map[string]any)
	routes, _ := srv0["routes"].([]any)
	if len(routes) == 0 {
		t.Error("routes array is empty — script-tag JSON was not parsed correctly as an object")
	}
}
