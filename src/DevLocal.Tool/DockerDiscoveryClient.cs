using System.Collections.Immutable;
using Docker.DotNet;
using Docker.DotNet.Models;
using DevLocal.Core;

namespace DevLocal.Tool;

internal static class DockerDiscoveryClient
{
    internal static async Task<ImmutableArray<ContainerInfo>> ListAsync(IDockerClient docker, CancellationToken token)
    {
        var summaries = await docker.Containers.ListContainersAsync(new ContainersListParameters { All = true }, token);
        return summaries.Select(Map).Where(info => info is not null).Cast<ContainerInfo>().ToImmutableArray();
    }

    internal static ContainerInfo? Map(ContainerListResponse summary)
    {
        var labels = summary.Labels ?? new Dictionary<string, string>();
        if (labels.TryGetValue("dev.local", out var disabled) &&
            (disabled.Equals("false", StringComparison.OrdinalIgnoreCase) ||
             disabled.Equals("0", StringComparison.OrdinalIgnoreCase) ||
             disabled.Equals("no", StringComparison.OrdinalIgnoreCase)))
            return null;

        var project = labels.TryGetValue("com.docker.compose.project", out var projectLabel) ? projectLabel : "";
        var service = labels.TryGetValue("com.docker.compose.service", out var serviceLabel) ? serviceLabel : "";
        var ports = summary.Ports ?? [];
        var published = ImmutableDictionary.CreateBuilder<ushort, ushort>();
        foreach (var port in ports)
            if (port.PublicPort is > 0)
                published[port.PrivatePort] = port.PublicPort.Value;

        var custom = ImmutableArray.CreateBuilder<CustomDomain>();
        if (labels.TryGetValue("dev.local.domains", out var domains))
        {
            foreach (var entry in domains.Split(';', StringSplitOptions.RemoveEmptyEntries))
            {
                var parts = entry.Trim().Split(':', 2);
                if (parts.Length == 2 && ushort.TryParse(parts[0], out var port) && port > 0 &&
                    !string.IsNullOrWhiteSpace(parts[1]))
                    custom.Add(new CustomDomain(port, parts[1].Trim()));
            }
        }

        var name = summary.Names?.FirstOrDefault(value => value.Length > 0);
        var id = summary.ID ?? "";
        return new ContainerInfo
        {
            ContainerId = id,
            ContainerName = name is null ? id[..Math.Min(id.Length, 12)] : name.TrimStart('/'),
            Image = summary.Image ?? "",
            Project = project,
            Service = service,
            IsCompose = project.Length > 0 && service.Length > 0,
            IsRunning = summary.State == "running",
            Ports = ports.Select(port => port.PrivatePort).Distinct().Order().ToImmutableArray(),
            PublishedPorts = published.ToImmutable(),
            Created = summary.Created == default ? null : new DateTimeOffset(summary.Created.ToUniversalTime()),
            Labels = labels.ToImmutableDictionary(StringComparer.Ordinal),
            CustomDomains = custom.ToImmutable(),
            Health = summary.Health?.Status ?? "",
            Networks = summary.NetworkSettings?.Networks?.Keys.Order(StringComparer.Ordinal).ToImmutableArray() ?? []
        };
    }
}
