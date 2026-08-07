package generator

import "strings"

const simpleIconsURL = "https://cdn.simpleicons.org/"

//nolint:goconst // image names and their simple-icons slugs intentionally coincide
var knownImageIcons = map[string]string{
	"nginx":               "nginx",
	"httpd":               "apache",
	"apache":              "apache",
	"caddy":               "caddy",
	"traefik":             "traefikproxy",
	"nginx-proxy-manager": "nginxproxymanager",
	"postgres":            "postgresql",
	"postgresql":          "postgresql",
	"mysql":               "mysql",
	"mariadb":             "mariadb",
	"redis":               "redis",
	"mongo":               "mongodb",
	"mongodb":             "mongodb",
	"node":                "nodedotjs",
	"nodejs":              "nodedotjs",
	"python":              "python",
	"go":                  "go",
	"golang":              "go",
	"rust":                "rust",
	"php":                 "php",
	"ruby":                "ruby",
	"openjdk":             "openjdk",
	"java":                "openjdk",
	"eclipse-temurin":     "openjdk",
	"dotnet":              "dotnet",
	"aspnet":              "dotnet",
	"elasticsearch":       "elasticsearch",
	"kafka":               "apachekafka",
	"rabbitmq":            "rabbitmq",
	"grafana":             "grafana",
	"prometheus":          "prometheus",
	"keycloak":            "keycloak",
	"vault":               "vault",
	"consul":              "consul",
	"minio":               "minio",
	"clickhouse":          "clickhouse",
	"clickhouse-server":   "clickhouse",
	"cockroachdb":         "cockroachlabs",
	"docker":              "docker",
	"git":                 "git",
	"github":              "github",
	"gitlab":              "gitlab",
	"gitea":               "gitea",
	"forgejo":             "forgejo",
	"wordpress":           "wordpress",
	"ghost":               "ghost",
	"nextcloud":           "nextcloud",
	"n8n":                 "n8n",
	"jenkins":             "jenkins",
	"jupyter":             "jupyter",
	"airflow":             "apacheairflow",
	"influxdb":            "influxdb",
	"portainer":           "portainer",
	"jellyfin":            "jellyfin",
	"plex":                "plex",
	"homeassistant":       "homeassistant",
	"pihole":              "pihole",
	"watchtower":          "watchtower",
	"adguard":             "adguard",
	"adguardhome":         "adguard",
	"ubuntu":              "ubuntu",
	"debian":              "debian",
	"fedora":              "fedora",
	"alpine":              "alpinelinux",
}

//nolint:goconst // images with official brand assets not on Simple Icons
var customImageIcons = map[string]string{
	"aspire-dashboard": "https://microsoft.github.io/aspire-brand/logo/aspire-icon-256.svg",
}

func iconForContainer(image string, labels map[string]string) string {
	if url := labelIconURL(labels); url != "" {
		return url
	}
	name := normalizeImageName(image)
	if url, ok := customImageIcons[name]; ok {
		return url
	}
	if slug, ok := knownImageIcons[name]; ok {
		return simpleIconsURL + slug
	}
	return ""
}

//nolint:goconst // open-standard label keys
func labelIconURL(labels map[string]string) string {
	for _, key := range []string{"org.opencontainers.image.logo", "com.docker.extension.icon"} {
		if v := strings.TrimSpace(getLabel(labels, key)); validIconURL(v) {
			return v
		}
	}
	return ""
}

func validIconURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func normalizeImageName(image string) string {
	image = strings.TrimSpace(image)
	if i := strings.LastIndex(image, "/"); i >= 0 {
		image = image[i+1:]
	}
	if i := strings.Index(image, "@"); i >= 0 {
		image = image[:i]
	}
	if i := strings.LastIndex(image, ":"); i >= 0 {
		image = image[:i]
	}
	return strings.ToLower(image)
}
