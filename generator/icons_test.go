package generator

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeImageName(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  string
	}{
		{"bare", "nginx", "nginx"},
		{"tag", "nginx:alpine", "nginx"},
		{"multi part tag", "nginx:1.27-alpine", "nginx"},
		{"dockerhub registry", "docker.io/nginx:alpine", "nginx"},
		{"library namespace", "docker.io/library/nginx:alpine", "nginx"},
		{"ghcr registry", "ghcr.io/org/app:v1", "app"},
		{"quay registry", "quay.io/org/repo:latest", "repo"},
		{"registry with port", "localhost:5000/myapp:1.0", "myapp"},
		{"digest", "repo@sha256:abc123", "repo"},
		{"tag and digest", "nginx:latest@sha256:abc123", "nginx"},
		{"uppercase", "Postgres:16", "postgres"},
		{"whitespace", "  nginx  ", "nginx"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeImageName(tt.image); got != tt.want {
				t.Errorf("normalizeImageName(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}

func TestImageRepository(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  string
	}{
		{"bare", "nginx", "nginx"},
		{"tag", "nginx:alpine", "nginx"},
		{"registry with path", "mcr.microsoft.com/mssql/server:2022-CU14-ubuntu-22.04", "mcr.microsoft.com/mssql/server"},
		{"registry port", "localhost:5000/myapp:1.0", "localhost:5000/myapp"},
		{"digest", "repo@sha256:abc123", "repo"},
		{"tag and digest", "nginx:latest@sha256:abc123", "nginx"},
		{"uppercase", "MCR.MICROSOFT.COM/MSSQL/SERVER:2022", "mcr.microsoft.com/mssql/server"},
		{"whitespace", "  nginx  ", "nginx"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := imageRepository(tt.image); got != tt.want {
				t.Errorf("imageRepository(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}

func TestValidIconURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"https", "https://example.com/logo.png", true},
		{"http", "http://example.com/logo.png", true},
		{"javascript", "javascript:alert(1)", false},
		{"file", "file:///etc/passwd", false},
		{"data", "data:image/svg+xml,<svg></svg>", false},
		{"relative", "/logo.png", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validIconURL(tt.url); got != tt.want {
				t.Errorf("validIconURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestLabelIconURL(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"nil labels", nil, ""},
		{"no labels", map[string]string{}, ""},
		{"oci logo", map[string]string{"org.opencontainers.image.logo": "https://example.com/logo.png"}, "https://example.com/logo.png"},
		{"docker extension icon", map[string]string{"com.docker.extension.icon": "https://example.com/icon.png"}, "https://example.com/icon.png"},
		{"oci logo takes priority", map[string]string{
			"org.opencontainers.image.logo": "https://example.com/logo.png",
			"com.docker.extension.icon":     "https://example.com/icon.png",
		}, "https://example.com/logo.png"},
		{"invalid scheme falls through", map[string]string{
			"org.opencontainers.image.logo": "javascript:alert(1)",
			"com.docker.extension.icon":     "https://example.com/icon.png",
		}, "https://example.com/icon.png"},
		{"invalid only", map[string]string{"org.opencontainers.image.logo": "javascript:alert(1)"}, ""},
		{"relative path", map[string]string{"org.opencontainers.image.logo": "logo.png"}, ""},
		{"empty value", map[string]string{"org.opencontainers.image.logo": ""}, ""},
		{"whitespace value", map[string]string{"org.opencontainers.image.logo": "  "}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := labelIconURL(tt.labels); got != tt.want {
				t.Errorf("labelIconURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIconForContainer(t *testing.T) {
	tests := []struct {
		name   string
		image  string
		labels map[string]string
		want   string
	}{
		{"known image", "postgres:16", nil, "https://cdn.simpleicons.org/postgresql"},
		{"known image with registry", "docker.io/library/nginx:alpine", nil, "https://cdn.simpleicons.org/nginx"},
		{"custom brand asset", "mcr.microsoft.com/dotnet/aspire-dashboard:latest", nil, "https://microsoft.github.io/aspire-brand/logo/aspire-icon-256.svg"},
		{"mssql server", "mcr.microsoft.com/mssql/server:2022-CU14-ubuntu-22.04", nil, "https://upload.wikimedia.org/wikipedia/commons/4/41/Microsoft_SQL_Server_2025_icon.svg"},
		{"unknown image", "my-custom-app:latest", nil, ""},
		{"empty image", "", nil, ""},
		{"label overrides known image", "nginx:alpine", map[string]string{
			"org.opencontainers.image.logo": "https://example.com/custom.png",
		}, "https://example.com/custom.png"},
		{"invalid label falls through to map", "redis:7", map[string]string{
			"org.opencontainers.image.logo": "relative/path.png",
		}, "https://cdn.simpleicons.org/redis"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := iconForContainer(tt.image, tt.labels); got != tt.want {
				t.Errorf("iconForContainer() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateIndexPageIcon(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		info     *ContainerInfo
		contains []string
		absent   []string
	}{
		{
			name: "known image renders icon",
			info: &ContainerInfo{
				ContainerName: "db",
				Image:         "postgres:16",
				Ports:         []uint16{5432},
				TargetKind:    targetDNS,
				IsRunning:     true,
				Created:       now,
			},
			contains: []string{`class="container-icon"`, "https://cdn.simpleicons.org/postgresql", `onerror="this.style.display='none'"`},
		},
		{
			name: "label icon renders",
			info: &ContainerInfo{
				ContainerName: "app",
				Image:         "myapp:latest",
				Ports:         []uint16{8080},
				TargetKind:    targetDNS,
				IsRunning:     true,
				Created:       now,
				Labels:        map[string]string{"com.docker.extension.icon": "https://example.com/icon.png"},
			},
			contains: []string{`class="container-icon"`, "https://example.com/icon.png"},
		},
		{
			name: "mssql renders icon",
			info: &ContainerInfo{
				ContainerName: "db",
				Image:         "mcr.microsoft.com/mssql/server:2022-CU14-ubuntu-22.04",
				Ports:         []uint16{1433},
				TargetKind:    targetDNS,
				IsRunning:     true,
				Created:       now,
			},
			contains: []string{`class="container-icon"`, "https://upload.wikimedia.org/wikipedia/commons/4/41/Microsoft_SQL_Server_2025_icon.svg"},
		},
		{
			name: "unknown image renders no icon",
			info: &ContainerInfo{
				ContainerName: "app",
				Image:         "my-custom-app:latest",
				Ports:         []uint16{8080},
				TargetKind:    targetDNS,
				IsRunning:     true,
				Created:       now,
			},
			absent: []string{`class="container-icon"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := GenerateIndexPage("dev.local", false, []*ContainerInfo{tt.info})
			for _, s := range tt.contains {
				if !strings.Contains(page, s) {
					t.Errorf("expected index page to contain %q", s)
				}
			}
			for _, s := range tt.absent {
				if strings.Contains(page, s) {
					t.Errorf("expected index page to not contain %q", s)
				}
			}
		})
	}
}
