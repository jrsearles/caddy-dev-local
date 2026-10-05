using System.Collections.Immutable;
using DevLocal.Core;

namespace DevLocal.Ui;

public sealed record UiSnapshot(
    ImmutableArray<ContainerInfo> Containers,
    DiscoveryStatus Status,
    string ConfigJson);

public sealed class UiState
{
    private readonly object gate = new();
    private UiSnapshot current = new([], new DiscoveryStatus(null, ""), "");
    private long revision;

    public event Action? Changed;

    public UiSnapshot Current
    {
        get
        {
            lock (gate)
                return current;
        }
    }

    public long Revision
    {
        get
        {
            lock (gate)
                return revision;
        }
    }

    public void Publish(DiscoveryUpdate update, string configJson)
    {
        lock (gate)
        {
            var canonical = Canonical(update);
            var next = new UiSnapshot(canonical.Snapshot, canonical.Status, configJson);
            if (next == current)
                return;
            current = next;
            revision++;
        }
        Changed?.Invoke();
    }

    private static DiscoveryUpdate Canonical(DiscoveryUpdate update) => update with
    {
        Snapshot = update.Snapshot.Select(info => info with
        {
            Ports = [.. info.Ports.Order()],
            Networks = [.. info.Networks.Order(StringComparer.Ordinal)],
            CustomDomains = [.. info.CustomDomains
                .OrderBy(domain => domain.Domain, StringComparer.Ordinal)
                .ThenBy(domain => domain.Port)]
        }).OrderBy(info => info.ContainerId, StringComparer.Ordinal).ToImmutableArray()
    };
}