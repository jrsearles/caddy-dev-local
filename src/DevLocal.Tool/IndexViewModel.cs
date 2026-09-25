using System.Globalization;
using System.Text.Encodings.Web;
using System.Text.Json;
using DevLocal.Core;

namespace DevLocal.Tool;

internal sealed record PortChip(string PortStr, string? CopyText, bool IsHTTP);
internal sealed record DomainEntry(string Domain, List<PortChip> Chips, string? URL);
internal sealed record PublishedPortPair(string Private, string Public);

internal sealed record IndexRow(
    List<DomainEntry>? Domains,
    string ContainerName,
    string ContainerID,
    string ContainerIDShort,
    string Image,
    string? Icon,
    string ComposeProject,
    string? ComposeService,
    bool IsRunning,
    bool ShowHealth,
    bool NoPorts,
    long StartedUnix,
    string StartedAbs,
    long? StoppedUnix,
    string StoppedAbs,
    string Health,
    List<string> Networks,
    List<PublishedPortPair> PublishedPortPairs,
    string LabelsJSON,
    string DomainsFlat,
    string DockerDesktopURL);

internal sealed record DisplayGroup(
    string Project,
    List<IndexRow> Rows,
    int RunningCount,
    int StoppedCount,
    bool OneService,
    string DockerDesktopURL,
    string DockerDesktopLabel);

internal sealed record IndexViewModel(
    string TLD,
    List<IndexRow>? DisplayRows,
    List<DisplayGroup>? Groups,
    int RunningCount,
    int StoppedCount,
    bool OneProject,
    string? ConfigJSON,
    string? ConfigJSONRaw,
    string? DiscoveryError,
    long? LastRefreshUnix);

internal static class IndexViewModelBuilder
{
    private static string Encode(string text) => HtmlEncoder.Default.Encode(text);

    internal static IndexViewModel Build(string tld, DiscoveryUpdate update, string configJson)
    {
        var rows = update.Snapshot.OrderBy(info => info.ContainerId, StringComparer.Ordinal)
            .Select(info => BuildRow(tld, info)).ToArray();
        var top = rows.Where(row => row.ComposeProject == "")
            .OrderByDescending(row => row.IsRunning).ToList();
        var groups = rows.Where(row => row.ComposeProject != "")
            .GroupBy(row => row.ComposeProject, StringComparer.Ordinal)
            .OrderBy(group => group.Key, StringComparer.Ordinal)
            .Select(group =>
            {
                var project = group.Key;
                var items = group.OrderByDescending(row => row.IsRunning).ToList();
                return new DisplayGroup(Encode(project), items, items.Count(row => row.IsRunning),
                    items.Count(row => !row.IsRunning), items.Count == 1, Encode("docker-desktop://dashboard/apps/" + project),
                    Encode("Open " + project + " in Docker Desktop"));
            }).ToList();

        return new IndexViewModel(
            Encode(tld), top.Count == 0 ? null : top, groups.Count == 0 ? null : groups,
            rows.Count(row => row.IsRunning), rows.Count(row => !row.IsRunning), groups.Count == 1,
            configJson == "" ? null : Encode(configJson), configJson == "" ? null : SafeConfigJson(configJson),
            update.Status.LastError == "" ? null : Encode(update.Status.LastError),
            update.Status.LastRefresh?.ToUnixTimeSeconds());
    }

    private static IndexRow BuildRow(string tld, ContainerInfo info)
    {
        var project = info.IsCompose ? info.Project : "";
        var service = info.IsCompose ? info.Service : "";
        var selected = info.SelectedPort > 0 ? info.SelectedPort.ToString(CultureInfo.InvariantCulture) : "";
        var domains = new List<DomainEntry>();
        var domainNames = new List<string>();
        if (!info.CustomDomains.IsDefaultOrEmpty)
        {
            foreach (var custom in info.CustomDomains)
            {
                domainNames.Add(custom.Domain);
                var effective = DomainGenerator.EffectivePort(info, custom.Port).ToString(CultureInfo.InvariantCulture);
                domains.Add(new DomainEntry(Encode(custom.Domain),
                    [new PortChip(effective, Encode(custom.Domain + ":" + effective), false)],
                    info.IsRunning ? Encode("https://" + custom.Domain) : null));
            }
        }
        else if (!info.Ports.IsDefaultOrEmpty)
        {
            var name = info.IsCompose ? info.Project + "." + info.Service : info.ContainerName;
            var ports = info.Ports.Select(port => DomainGenerator.EffectivePort(info, port)).ToArray();
            foreach (var domain in new[] { name + "." + tld, name + ".localhost" })
            {
                domainNames.Add(domain);
                var url = info.IsRunning && info.SelectedPort > 0 ? Encode("https://" + domain) : null;
                var chips = ports.OrderBy(port => port.ToString(CultureInfo.InvariantCulture) == selected ? 0 : 1)
                    .ThenBy(port => port).Select(port =>
                    {
                        var text = port.ToString(CultureInfo.InvariantCulture);
                        return new PortChip(text, Encode(domain + ":" + text), text == selected && url is not null);
                    }).ToList();
                if (chips.Count == 0)
                    chips.Add(new PortChip("-", null, false));
                domains.Add(new DomainEntry(Encode(domain), chips, url));
            }
        }

        var ordered = info.PublishedPorts.OrderBy(entry => entry.Key)
            .Select(entry => new PublishedPortPair(entry.Key.ToString(CultureInfo.InvariantCulture),
                entry.Value.ToString(CultureInfo.InvariantCulture))).ToList();
        var labels = info.Labels.Where(entry => new[]
            {
                "dev.local.", "com.docker.compose.", "org.opencontainers.image.", "com.docker.extension."
            }.Any(prefix => entry.Key.StartsWith(prefix, StringComparison.Ordinal)))
            .OrderBy(entry => entry.Key, StringComparer.Ordinal)
            .ToDictionary(entry => entry.Key, entry => entry.Value, StringComparer.Ordinal);
        var labelsJson = Encode(JsonSerializer.Serialize(labels));
        var created = info.Created;
        var stopped = !info.IsRunning ? info.LastStopped : null;
        var icon = ImageIcons.For(info.Image, info.Labels);
        return new IndexRow(
            domains.Count == 0 ? null : domains,
            Encode(info.ContainerName), Encode(info.ContainerId), Encode(info.ContainerId[..Math.Min(12, info.ContainerId.Length)]),
            Encode(info.Image), icon == "" ? null : Encode(icon), Encode(project), service == "" ? null : Encode(service), info.IsRunning,
            info.IsRunning && info.Health is not ("" or "none"),
            info.Ports.IsDefaultOrEmpty && info.CustomDomains.IsDefaultOrEmpty,
            created?.ToUnixTimeSeconds() ?? 0, Encode(Absolute(created)),
            stopped?.ToUnixTimeSeconds(), Encode(Absolute(stopped)), Encode(info.Health),
            info.Networks.Select(Encode).ToList(), ordered, labelsJson,
            Encode(string.Join(' ', domainNames)),
            Encode("docker-desktop://dashboard/logs?containerIds=" + info.ContainerId));
    }

    private static string Absolute(DateTimeOffset? time) =>
        time?.ToLocalTime().ToString("yyyy-MM-dd HH:mm", CultureInfo.InvariantCulture) ?? "";

    private static string SafeConfigJson(string configJson)
    {
        if (configJson == "")
            return "";
        try
        {
            using var parsed = JsonDocument.Parse(configJson);
            return JsonSerializer.Serialize(parsed.RootElement);
        }
        catch (JsonException)
        {
            return configJson.Replace("&", "\\u0026", StringComparison.Ordinal)
                .Replace("<", "\\u003c", StringComparison.Ordinal)
                .Replace(">", "\\u003e", StringComparison.Ordinal);
        }
    }
}
