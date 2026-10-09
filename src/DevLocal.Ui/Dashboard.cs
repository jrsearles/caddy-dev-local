using System.Globalization;
using DevLocal.Core;

namespace DevLocal.Ui;

public sealed record PortChipModel(string Port, bool IsSelected, bool IsHttp, string CopyText);

public sealed record DomainModel(string Domain, string Url, List<PortChipModel> Ports);

public sealed record ContainerModel(
    string Id,
    string ShortId,
    string Name,
    string Image,
    string Icon,
    string Project,
    string Service,
    bool IsCompose,
    bool IsRunning,
    string Health,
    bool ShowHealth,
    List<DomainModel> Domains,
    bool NoPorts,
    DateTimeOffset? Started,
    DateTimeOffset? Stopped,
    List<string> Networks,
    List<PublishedPortModel> PublishedPorts,
    List<LabelModel> Labels,
    string DockerDesktopUrl);

public sealed record PublishedPortModel(string ContainerPort, string HostPort);

public sealed record LabelModel(string Key, string Value);

public sealed record ProjectModel(
    string Project,
    List<ContainerModel> Containers,
    int Running,
    int Stopped,
    string DockerDesktopUrl,
    string DockerDesktopLabel);

public sealed record RouteModel(
    string Id,
    List<string> Hosts,
    List<string> Paths,
    List<string> Handlers,
    List<string> Upstreams,
    string FileRoot,
    string Tls,
    string TlsClass);

public sealed record DashboardModel(
    string Tld,
    List<ContainerModel> Standalone,
    List<ProjectModel> Projects,
    int Running,
    int Stopped,
    string? Error,
    DateTimeOffset? LastRefresh,
    bool HasConfig,
    List<RouteModel> Routes,
    string ConfigJson)
{
    public bool IsEmpty => Standalone.Count == 0 && Projects.Count == 0;

    public int RouteCount => Routes.Count;

    public string[] FilterTerms(string filter) => filter.Trim().Split(
        ' ', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries);

    public bool Matches(ContainerModel container, string[] terms) => terms.All(term =>
        (container.Name + " " + container.Image + " " + container.Project + " " + container.Service + " "
            + string.Join(" ", container.Domains.Select(domain => domain.Domain)))
            .Contains(term, StringComparison.OrdinalIgnoreCase));

    public bool HasDomain(string domain) =>
        Standalone.Concat(Projects.SelectMany(project => project.Containers))
            .Any(container => container.Domains.Any(entry => entry.Domain == domain));
}

public static class DashboardBuilder
{
    public static DashboardModel Empty { get; } = Build("", new UiSnapshot([], new DiscoveryStatus(null, ""), ""));

    public static DashboardModel Build(string tld, UiSnapshot snapshot)
    {
        var rows = snapshot.Containers.Select(info => BuildContainer(tld, info)).ToList();
        var standalone = rows.Where(row => row.Project == "")
            .OrderByDescending(row => row.IsRunning).ToList();
        var projects = rows.Where(row => row.Project != "")
            .GroupBy(row => row.Project, StringComparer.Ordinal)
            .OrderBy(group => group.Key, StringComparer.Ordinal)
            .Select(group =>
            {
                var items = group.OrderByDescending(row => row.IsRunning).ToList();
                return new ProjectModel(group.Key, items,
                    items.Count(row => row.IsRunning), items.Count(row => !row.IsRunning),
                    "docker-desktop://dashboard/apps/" + group.Key,
                    "Open " + group.Key + " in Docker Desktop");
            })
            .ToList();

        return new DashboardModel(tld, standalone, projects,
            rows.Count(row => row.IsRunning), rows.Count(row => !row.IsRunning),
            snapshot.Status.LastError == "" ? null : snapshot.Status.LastError,
            snapshot.Status.LastRefresh, snapshot.ConfigJson != "",
            CaddyConfig.ParseRoutes(snapshot.ConfigJson), snapshot.ConfigJson);
    }

    private static ContainerModel BuildContainer(string tld, ContainerInfo info)
    {
        var project = info.IsCompose ? info.Project : "";
        var service = info.IsCompose ? info.Service : "";
        var domains = new List<DomainModel>();

        if (!info.CustomDomains.IsDefaultOrEmpty)
        {
            foreach (var custom in info.CustomDomains)
            {
                var port = DomainGenerator.EffectivePort(info, custom.Port)
                    .ToString(CultureInfo.InvariantCulture);
                domains.Add(new DomainModel(custom.Domain, "https://" + custom.Domain,
                    [new PortChipModel(port, true, false, custom.Domain + ":" + port)]));
            }
        }
        else if (!info.Ports.IsDefaultOrEmpty)
        {
            var name = info.IsCompose ? info.Project + "." + info.Service : info.ContainerName;
            var ports = info.Ports
                .Select(port => DomainGenerator.EffectivePort(info, port))
                .ToArray();
            foreach (var domain in new[] { name + "." + tld, name + ".localhost" })
            {
                var chips = ports
                    .OrderBy(port => port == info.SelectedPort ? 0 : 1)
                    .ThenBy(port => port)
                    .Select(port =>
                    {
                        var text = port.ToString(CultureInfo.InvariantCulture);
                        return new PortChipModel(text, port == info.SelectedPort,
                            port == info.SelectedPort, domain + ":" + text);
                    })
                    .ToList();
                if (chips.Count == 0)
                    chips.Add(new PortChipModel("-", false, false, ""));
                domains.Add(new DomainModel(domain, "https://" + domain, chips));
            }
        }

        var published = info.PublishedPorts.OrderBy(entry => entry.Key)
            .Select(entry => new PublishedPortModel(
                entry.Key.ToString(CultureInfo.InvariantCulture),
                entry.Value.ToString(CultureInfo.InvariantCulture)))
            .ToList();
        var labels = info.Labels
            .Where(entry => new[]
            {
                "dev.local.", "com.docker.compose.", "org.opencontainers.image.", "com.docker.extension."
            }.Any(prefix => entry.Key.StartsWith(prefix, StringComparison.Ordinal)))
            .OrderBy(entry => entry.Key, StringComparer.Ordinal)
            .Select(entry => new LabelModel(entry.Key, entry.Value))
            .ToList();
        var stopped = !info.IsRunning && info.LastStopped is { } value && value != default
            ? (DateTimeOffset?)value
            : null;

        return new ContainerModel(
            info.ContainerId, info.ContainerId[..Math.Min(12, info.ContainerId.Length)],
            info.ContainerName, info.Image, ImageIcons.For(info.Image, info.Labels),
            project, service, info.IsCompose, info.IsRunning, info.Health,
            info.IsRunning && info.Health is not ("" or "none"),
            domains, info.Ports.IsDefaultOrEmpty && info.CustomDomains.IsDefaultOrEmpty,
            info.Created, stopped, [.. info.Networks.Order(StringComparer.Ordinal)], published, labels,
            "docker-desktop://dashboard/logs?containerIds=" + info.ContainerId);
    }
}