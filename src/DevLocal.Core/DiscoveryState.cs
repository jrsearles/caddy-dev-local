using System.Collections.Immutable;

namespace DevLocal.Core;

public interface IContainerSnapshotSource
{
    Task<ImmutableArray<ContainerInfo>> ListAsync(CancellationToken token);
}

public interface IPortProbe
{
    Task<ushort> ProbeAsync(string host, IReadOnlyList<ushort> ports, TimeSpan timeout, CancellationToken token);
}

public sealed class DiscoveryState(
    IContainerSnapshotSource source,
    TimeSpan staleTtl,
    TimeSpan probeTimeout,
    IPortProbe? probe = null,
    TimeProvider? timeProvider = null)
{
    private readonly TimeProvider clock = timeProvider ?? TimeProvider.System;
    private readonly SemaphoreSlim refreshGate = new(1, 1);
    private readonly object stateGate = new();
    private ImmutableDictionary<string, ContainerInfo> containers = ImmutableDictionary<string, ContainerInfo>.Empty;
    private ImmutableDictionary<string, ushort> selectedPorts = ImmutableDictionary<string, ushort>.Empty;
    private DiscoveryUpdate current = new([], new DiscoveryStatus(null, ""));

    public DiscoveryUpdate Snapshot()
    {
        lock (stateGate)
            return current;
    }

    public bool HasPendingPorts()
    {
        lock (stateGate)
            return current.Snapshot.Any(container =>
                container.IsRunning
                && container.SelectedPort == 0
                && container.Labels.GetValueOrDefault("dev.local.domains") is not { Length: > 0 }
                && container.Ports.Any(port =>
                    container.PublishedPorts.TryGetValue(port, out var published) && published != 0));
    }

    public DiscoveryUpdate ReportError(string message)
    {
        lock (stateGate)
        {
            current = current with
            {
                Status = current.Status with { LastError = message, LastErrorAt = clock.GetLocalNow() }
            };
            return current;
        }
    }

    public async Task<DiscoveryRefreshResult> RefreshAsync(CancellationToken token)
    {
        await refreshGate.WaitAsync(token);
        try
        {
            ImmutableArray<ContainerInfo> listed;
            try
            {
                listed = await source.ListAsync(token);
            }
            catch (OperationCanceledException) when (token.IsCancellationRequested)
            {
                throw;
            }
            catch (Exception ex)
            {
                lock (stateGate)
                {
                    current = current with
                    {
                        Status = current.Status with { LastError = ex.Message, LastErrorAt = clock.GetLocalNow() }
                    };
                    return new DiscoveryRefreshResult(current, ex);
                }
            }

            ImmutableDictionary<string, ContainerInfo> before;
            ImmutableDictionary<string, ushort>.Builder cache;
            lock (stateGate)
            {
                before = containers;
                cache = selectedPorts.ToBuilder();
            }

            var now = clock.GetLocalNow();
            var next = before.ToBuilder();
            var seen = new HashSet<string>(StringComparer.Ordinal);
            foreach (var info in listed)
            {
                seen.Add(info.ContainerId);
                if (info.IsRunning)
                {
                    next[info.ContainerId] = info with { LastStopped = null };
                    continue;
                }

                var previous = before.GetValueOrDefault(info.ContainerId);
                next[info.ContainerId] = info with
                {
                    Ports = previous?.Ports ?? info.Ports,
                    PublishedPorts = previous?.PublishedPorts ?? info.PublishedPorts,
                    LastStopped = previous is null || previous.IsRunning ? now : previous.LastStopped
                };
            }

            foreach (var (id, info) in next.ToImmutable())
            {
                if (!seen.Contains(id) && (info.IsRunning || info.LastStopped is { } stopped && now - stopped > staleTtl))
                    next.Remove(id);
            }

            var candidates = next.Values.OrderBy(item => item.ContainerName, StringComparer.Ordinal)
                .ThenBy(item => item.ContainerId, StringComparer.Ordinal)
                .ToArray();
            var selections = new ushort[candidates.Length];
            for (var index = 0; index < candidates.Length; index++)
            {
                if (!candidates[index].IsRunning)
                    cache.TryGetValue(candidates[index].ContainerId, out selections[index]);
            }

            await Parallel.ForEachAsync(
                Enumerable.Range(0, candidates.Length).Where(index => candidates[index].IsRunning),
                cancellationToken: token,
                body: async (index, cancellation) => selections[index] =
                    await SelectPortAsync(candidates[index], cancellation));

            for (var index = 0; index < candidates.Length; index++)
            {
                if (selections[index] != 0)
                    cache[candidates[index].ContainerId] = selections[index];
                next[candidates[index].ContainerId] = candidates[index] with { SelectedPort = selections[index] };
            }

            async Task<ushort> SelectPortAsync(ContainerInfo info, CancellationToken cancellation)
            {
                if (probe is null)
                    return 0;
                if (info.Labels.TryGetValue("dev.local.domains", out var label) && label != "")
                    return 0;
                var published = info.Ports
                    .Where(port => info.PublishedPorts.TryGetValue(port, out var publicPort) && publicPort != 0)
                    .Select(port => info.PublishedPorts[port])
                    .Distinct().ToArray();
                if (published.Length == 0)
                    return 0;
                try
                {
                    return await probe.ProbeAsync("localhost", published, probeTimeout, cancellation);
                }
                catch (OperationCanceledException) when (cancellation.IsCancellationRequested)
                {
                    throw;
                }
                catch
                {
                    return 0;
                }
            }

            foreach (var id in cache.Keys.ToArray())
                if (!next.ContainsKey(id))
                    cache.Remove(id);

            token.ThrowIfCancellationRequested();
            var update = new DiscoveryUpdate(
                next.Values.OrderBy(item => item.ContainerName, StringComparer.Ordinal)
                    .ThenBy(item => item.ContainerId, StringComparer.Ordinal).ToImmutableArray(),
                new DiscoveryStatus(clock.GetLocalNow(), ""));
            lock (stateGate)
            {
                containers = next.ToImmutable();
                selectedPorts = cache.ToImmutable();
                current = update;
            }
            return new DiscoveryRefreshResult(update, null);
        }
        finally
        {
            refreshGate.Release();
        }
    }
}
