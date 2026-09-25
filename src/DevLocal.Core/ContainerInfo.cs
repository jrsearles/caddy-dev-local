using System.Collections.Immutable;

namespace DevLocal.Core;

public sealed record CustomDomain(ushort Port, string Domain);

public sealed record ContainerInfo
{
    public string ContainerId { get; init; } = "";
    public string ContainerName { get; init; } = "";
    public string Image { get; init; } = "";
    public string Project { get; init; } = "";
    public string Service { get; init; } = "";
    public ImmutableArray<ushort> Ports { get; init; } = [];
    public ImmutableDictionary<ushort, ushort> PublishedPorts { get; init; } = ImmutableDictionary<ushort, ushort>.Empty;
    public ushort SelectedPort { get; init; }
    public bool IsCompose { get; init; }
    public bool IsRunning { get; init; }
    public DateTimeOffset? LastStopped { get; init; }
    public DateTimeOffset? Created { get; init; }
    public ImmutableDictionary<string, string> Labels { get; init; } = ImmutableDictionary<string, string>.Empty;
    public ImmutableArray<CustomDomain> CustomDomains { get; init; } = [];
    public string Health { get; init; } = "";
    public ImmutableArray<string> Networks { get; init; } = [];
}
