package generator

import (
	"strings"
	"testing"
	"time"
)

func TestRenderSanity(t *testing.T) {
	now := time.Now()
	containers := []*ContainerInfo{
		{
			ContainerName: "my-nginx",
			Image:         "nginx:latest",
			Ports:         []uint16{80},
			SelectedPort:  80,
			TargetKind:    targetGateway,
			IsRunning:     true,
			Created:       now,
		},
		{
			ContainerName: "web-1",
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
		{
			ContainerName: "db-1",
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

	page := GenerateIndexPage("dev.local", false, containers, "")

	checks := []string{
		`<title>devlocal — Container Index</title>`,
		`fonts.googleapis.com`,
		`class="card"`,
		`class="container-icon"`,
		`data-name="my-nginx"`,
		`data-project="demo"`,
		`class="project-section"`,
		`class="project-toggle"`,
		`class="project-cards"`,
		`class="mini-chip mini-chip-running"`,
		`class="mini-chip mini-chip-stopped"`,
		`aria-expanded="false"`,
		`id="search"`,
		`id="liveDot"`,
		`status-running`,
		`status-stopped`,
		`data-copy=`,
		`sessionStorage.setItem('devlocal-open'`,
		`fetch(location.href`,
		`>2 running<`,
		`>1 stopped<`,
		`>1 project<`,
	}
	for _, c := range checks {
		if !strings.Contains(page, c) {
			t.Errorf("page missing %q", c)
		}
	}
	if strings.Contains(page, `class="config-panel"`) {
		t.Error("config panel rendered with empty config")
	}
	if strings.Contains(page, `class="project-header"`) {
		t.Error("old table layout still present")
	}
	if strings.Contains(page, "rowspan") {
		t.Error("rowspan table logic still present")
	}
	if strings.Contains(page, "displayRow") {
		t.Error("displayRow still referenced")
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
	page := GenerateIndexPage("dev.local", false, nil, configJSON)

	checks := []string{
		`class="config-panel"`,
		`class="config-toggle"`,
		`class="config-pre"`,
		`onclick="toggleConfig(this)"`,
		`aria-expanded="false"`,
		`function toggleConfig(btn)`,
		`&#34;srv0&#34;`,
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
