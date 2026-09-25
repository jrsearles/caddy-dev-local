using System.Collections.Immutable;

namespace DevLocal.Core;

public sealed record DiscoveryStatus(DateTimeOffset? LastRefresh, string LastError, DateTimeOffset? LastErrorAt = null);

public sealed record DiscoveryUpdate(ImmutableArray<ContainerInfo> Snapshot, DiscoveryStatus Status);

public sealed record DiscoveryRefreshResult(DiscoveryUpdate Update, Exception? Error);
