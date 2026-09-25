using System.Collections.Immutable;

namespace DevLocal.Core;

public static class DomainGenerator
{
    public static IReadOnlyDictionary<string, ImmutableArray<string>> DomainTargets(string tld, IEnumerable<ContainerInfo> containers)
    {
        var targets = new SortedDictionary<string, List<string>>(StringComparer.Ordinal);

        void Add(string domain, string target)
        {
            if (!targets.TryGetValue(domain, out var values))
                targets[domain] = values = [];
            values.Add(target);
        }

        foreach (var container in containers)
        {
            if (!container.IsRunning)
                continue;

            if (!container.CustomDomains.IsDefaultOrEmpty)
            {
                foreach (var custom in container.CustomDomains)
                    Add(custom.Domain, $"localhost:{EffectivePort(container, custom.Port)}");
                continue;
            }

            if (container.SelectedPort == 0)
                continue;

            var target = $"localhost:{container.SelectedPort}";
            Add(ContainerDomain(tld, container), target);
            Add(LocalhostDomain(container), target);
        }

        return targets.ToDictionary(
            entry => entry.Key,
            entry => entry.Value.OrderBy(value => value, StringComparer.Ordinal).ToImmutableArray(),
            StringComparer.Ordinal);
    }

    public static ImmutableArray<string> Domains(string tld, IEnumerable<ContainerInfo> containers)
    {
        var domains = new SortedSet<string>(StringComparer.Ordinal);
        foreach (var container in containers)
        {
            if (!container.IsRunning)
                continue;

            if (!container.CustomDomains.IsDefaultOrEmpty)
            {
                foreach (var custom in container.CustomDomains)
                    domains.Add(custom.Domain);
                continue;
            }

            if (container.SelectedPort == 0 && container.PublishedPorts.Count == 0)
                continue;

            domains.Add(ContainerDomain(tld, container));
            domains.Add(LocalhostDomain(container));
        }

        return domains.ToImmutableArray();
    }

    public static string TldLocalhost(string tld)
    {
        if (tld.Equals("localhost", StringComparison.OrdinalIgnoreCase) ||
            tld.EndsWith(".localhost", StringComparison.OrdinalIgnoreCase))
            return tld;
        return tld.Split('.')[0] + ".localhost";
    }

    public static ushort EffectivePort(ContainerInfo container, ushort privatePort) =>
        container.PublishedPorts.TryGetValue(privatePort, out var published) ? published : privatePort;

    private static string ContainerDomain(string tld, ContainerInfo container) =>
        container.IsCompose ? $"{container.Project}.{container.Service}.{tld}" : $"{container.ContainerName}.{tld}";

    private static string LocalhostDomain(ContainerInfo container) =>
        container.IsCompose ? $"{container.Project}.{container.Service}.localhost" : $"{container.ContainerName}.localhost";
}
