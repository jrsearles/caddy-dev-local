using System.Collections.Immutable;

namespace DevLocal.Core;

public sealed record DiscoveryStatus(DateTimeOffset? LastRefresh, string LastError);

public sealed record DiscoveryUpdate(ImmutableArray<ContainerInfo> Snapshot, DiscoveryStatus Status);
