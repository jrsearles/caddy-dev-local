using System.Collections.Immutable;

namespace DevLocal.Tool;

internal static class ImageIcons
{
    private static readonly Dictionary<string, string> known = new(StringComparer.Ordinal)
    {
        ["nginx"] = "nginx",
        ["httpd"] = "apache",
        ["apache"] = "apache",
        ["caddy"] = "caddy",
        ["traefik"] = "traefikproxy",
        ["nginx-proxy-manager"] = "nginxproxymanager",
        ["postgres"] = "postgresql",
        ["postgresql"] = "postgresql",
        ["mysql"] = "mysql",
        ["mariadb"] = "mariadb",
        ["redis"] = "redis",
        ["mongo"] = "mongodb",
        ["node"] = "nodedotjs",
        ["nodejs"] = "nodedotjs",
        ["python"] = "python",
        ["go"] = "go",
        ["golang"] = "go",
        ["rust"] = "rust",
        ["php"] = "php",
        ["ruby"] = "ruby",
        ["openjdk"] = "openjdk",
        ["java"] = "openjdk",
        ["eclipse-temurin"] = "openjdk",
        ["dotnet"] = "dotnet",
        ["aspnet"] = "dotnet",
        ["elasticsearch"] = "elasticsearch",
        ["kafka"] = "apachekafka",
        ["rabbitmq"] = "rabbitmq",
        ["grafana"] = "grafana",
        ["prometheus"] = "prometheus",
        ["keycloak"] = "keycloak",
        ["vault"] = "vault",
        ["consul"] = "consul",
        ["minio"] = "minio",
        ["clickhouse"] = "clickhouse",
        ["clickhouse-server"] = "clickhouse",
        ["cockroachdb"] = "cockroachlabs",
        ["docker"] = "docker",
        ["git"] = "git",
        ["github"] = "github",
        ["gitlab"] = "gitlab",
        ["gitea"] = "gitea",
        ["forgejo"] = "forgejo",
        ["wordpress"] = "wordpress",
        ["ghost"] = "ghost",
        ["nextcloud"] = "nextcloud",
        ["n8n"] = "n8n",
        ["jenkins"] = "jenkins",
        ["jupyter"] = "jupyter",
        ["airflow"] = "apacheairflow",
        ["influxdb"] = "influxdb",
        ["portainer"] = "portainer",
        ["jellyfin"] = "jellyfin",
        ["plex"] = "plex",
        ["homeassistant"] = "homeassistant",
        ["pihole"] = "pihole",
        ["watchtower"] = "watchtower",
        ["adguard"] = "adguard",
        ["adguardhome"] = "adguard",
        ["ubuntu"] = "ubuntu",
        ["debian"] = "debian",
        ["fedora"] = "fedora",
        ["alpine"] = "alpinelinux"
    };

    internal static string For(string image, ImmutableDictionary<string, string> labels)
    {
        foreach (var key in new[] { "org.opencontainers.image.logo", "com.docker.extension.icon" })
        {
            if (labels.TryGetValue(key, out var icon))
            {
                icon = icon.Trim();
                if (icon.StartsWith("http://", StringComparison.Ordinal) ||
                    icon.StartsWith("https://", StringComparison.Ordinal))
                    return icon;
            }
        }

        var repository = image.Trim().ToLowerInvariant().Split('@')[0];
        var colon = repository.LastIndexOf(':');
        if (colon >= 0 && !repository[(colon + 1)..].Contains('/'))
            repository = repository[..colon];
        if (repository == "mcr.microsoft.com/mssql/server")
            return "https://upload.wikimedia.org/wikipedia/commons/4/41/Microsoft_SQL_Server_2025_icon.svg";

        var name = image.Trim().Split('/').Last().Split('@')[0].Split(':')[0].ToLowerInvariant();
        if (name == "aspire-dashboard")
            return "https://microsoft.github.io/aspire-brand/logo/aspire-icon-256.svg";
        return known.TryGetValue(name, out var slug) ? "https://cdn.simpleicons.org/" + slug : "";
    }
}
