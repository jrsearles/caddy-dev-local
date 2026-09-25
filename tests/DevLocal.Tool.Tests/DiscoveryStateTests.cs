using System.Collections.Immutable;
using DevLocal.Core;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class DiscoveryStateTests
{
    [Fact]
    public async Task StoppedContainersKeepPublishedPortsAndCachedSelectionUntilDisappearedAndStale()
    {
        var source = new FakeSource();
        var clock = new FakeClock();
        var probe = new FakeProbe();
        var discovery = new DiscoveryState(source, TimeSpan.FromHours(1), TimeSpan.FromSeconds(2), probe, clock);
        source.Enqueue([Container("web", 8080)]);
        var running = (await discovery.RefreshAsync(CancellationToken.None)).Update.Snapshot[0];
        Assert.Equal((ushort)8080, running.SelectedPort);

        source.Enqueue([new ContainerInfo { ContainerId = "web", ContainerName = "web" }]);
        clock.Advance(TimeSpan.FromMinutes(1));
        var stopped = (await discovery.RefreshAsync(CancellationToken.None)).Update.Snapshot[0];
        Assert.Equal((ushort)8080, stopped.SelectedPort);
        Assert.Equal((ushort)8080, stopped.PublishedPorts[80]);
        Assert.NotNull(stopped.LastStopped);

        clock.Advance(TimeSpan.FromHours(2));
        source.Enqueue([new ContainerInfo { ContainerId = "web", ContainerName = "web" }]);
        Assert.Single((await discovery.RefreshAsync(CancellationToken.None)).Update.Snapshot);
        source.Enqueue([]);
        Assert.Empty((await discovery.RefreshAsync(CancellationToken.None)).Update.Snapshot);

        source.Enqueue([new ContainerInfo { ContainerId = "web", ContainerName = "web" }]);
        Assert.Equal((ushort)0, (await discovery.RefreshAsync(CancellationToken.None)).Update.Snapshot[0].SelectedPort);
    }

    [Fact]
    public async Task ListFailureRetainsCompletePreviousSnapshotAndReportsRecovery()
    {
        var source = new FakeSource();
        var clock = new FakeClock();
        var discovery = new DiscoveryState(source, TimeSpan.FromHours(1), TimeSpan.FromSeconds(2), timeProvider: clock);
        source.Enqueue([Container("web", 8080)]);
        var first = await discovery.RefreshAsync(CancellationToken.None);
        source.Enqueue(new IOException("Docker Desktop is unavailable"));
        clock.Advance(TimeSpan.FromMinutes(1));

        var failed = await discovery.RefreshAsync(CancellationToken.None);
        Assert.IsType<IOException>(failed.Error);
        Assert.Equal(first.Update.Snapshot, failed.Update.Snapshot);
        Assert.Equal(first.Update.Status.LastRefresh, failed.Update.Status.LastRefresh);
        Assert.NotNull(failed.Update.Status.LastErrorAt);
        Assert.Contains("unavailable", discovery.Snapshot().Status.LastError);

        source.Enqueue([]);
        var recovered = await discovery.RefreshAsync(CancellationToken.None);
        Assert.Null(recovered.Error);
        Assert.Empty(recovered.Update.Snapshot);
        Assert.Equal("", recovered.Update.Status.LastError);
        Assert.Null(recovered.Update.Status.LastErrorAt);
    }

    [Fact]
    public async Task ProbeDoesNotExposePartiallyEnrichedCandidateAndRefreshesAreSerialized()
    {
        var source = new FakeSource();
        var probe = new FakeProbe();
        var discovery = new DiscoveryState(source, TimeSpan.FromHours(1), TimeSpan.FromSeconds(2), probe);
        source.Enqueue([Container("web", 8080)]);
        await discovery.RefreshAsync(CancellationToken.None);

        var entered = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var release = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        probe.Wait = async () =>
        {
            entered.TrySetResult();
            await release.Task;
        };
        source.Enqueue([Container("web", 8181)]);
        var pending = discovery.RefreshAsync(CancellationToken.None);
        await entered.Task.WaitAsync(TimeSpan.FromSeconds(5));
        source.Enqueue([Container("web", 8282)]);
        var queued = discovery.RefreshAsync(CancellationToken.None);

        Assert.Equal((ushort)8080, discovery.Snapshot().Snapshot[0].SelectedPort);
        Assert.Equal(2, source.Calls);

        release.SetResult();
        Assert.Equal((ushort)8181, (await pending).Update.Snapshot[0].SelectedPort);
        Assert.Equal((ushort)8282, (await queued).Update.Snapshot[0].SelectedPort);
        Assert.Equal(3, source.Calls);
    }

    [Fact]
    public async Task CustomDomainLabelsSkipProbeAndRunningDisappearanceRemovesContainerImmediately()
    {
        var source = new FakeSource();
        var probe = new FakeProbe();
        var discovery = new DiscoveryState(source, TimeSpan.FromHours(1), TimeSpan.FromSeconds(2), probe);
        source.Enqueue([Container("web", 8080) with
        {
            Labels = ImmutableDictionary<string, string>.Empty.Add("dev.local.domains", "80:web.custom.local")
        }]);
        Assert.Equal((ushort)0, (await discovery.RefreshAsync(CancellationToken.None)).Update.Snapshot[0].SelectedPort);
        Assert.Equal(0, probe.Calls);

        source.Enqueue([]);
        Assert.Empty((await discovery.RefreshAsync(CancellationToken.None)).Update.Snapshot);
    }

    [Fact]
    public async Task FailedProbeDoesNotUseCachedPortForRunningContainerButRestoresItWhenStopped()
    {
        var source = new FakeSource();
        var probe = new FakeProbe();
        var discovery = new DiscoveryState(source, TimeSpan.FromHours(1), TimeSpan.FromSeconds(2), probe);
        source.Enqueue([Container("web", 8080)]);
        Assert.Equal((ushort)8080, (await discovery.RefreshAsync(CancellationToken.None)).Update.Snapshot[0].SelectedPort);

        probe.Error = new HttpRequestException("connection refused");
        source.Enqueue([Container("web", 8181)]);
        Assert.Equal((ushort)0, (await discovery.RefreshAsync(CancellationToken.None)).Update.Snapshot[0].SelectedPort);

        source.Enqueue([new ContainerInfo { ContainerId = "web", ContainerName = "web" }]);
        Assert.Equal((ushort)8080, (await discovery.RefreshAsync(CancellationToken.None)).Update.Snapshot[0].SelectedPort);
    }

    [Fact]
    public async Task CancellationWhileProbingLeavesLastCompleteSnapshotUntouched()
    {
        var source = new FakeSource();
        var probe = new FakeProbe();
        var discovery = new DiscoveryState(source, TimeSpan.FromHours(1), TimeSpan.FromSeconds(2), probe);
        source.Enqueue([Container("web", 8080)]);
        var first = await discovery.RefreshAsync(CancellationToken.None);

        using var cancellation = new CancellationTokenSource();
        probe.Wait = () => { cancellation.Cancel(); return Task.CompletedTask; };
        source.Enqueue([Container("web", 8181)]);
        await Assert.ThrowsAnyAsync<OperationCanceledException>(() => discovery.RefreshAsync(cancellation.Token));

        Assert.Equal(first.Update, discovery.Snapshot());
    }

    private static ContainerInfo Container(string id, ushort published) => new()
    {
        ContainerId = id,
        ContainerName = id,
        IsRunning = true,
        Ports = [80],
        PublishedPorts = ImmutableDictionary<ushort, ushort>.Empty.Add(80, published)
    };

    private sealed class FakeClock : TimeProvider
    {
        private DateTimeOffset now = new(2026, 9, 25, 0, 0, 0, TimeSpan.Zero);
        public override DateTimeOffset GetUtcNow() => now;
        public void Advance(TimeSpan interval) => now += interval;
    }

    private sealed class FakeSource : IContainerSnapshotSource
    {
        private readonly Queue<object> results = new();
        public int Calls { get; private set; }
        public void Enqueue(ImmutableArray<ContainerInfo> snapshot) => results.Enqueue(snapshot);
        public void Enqueue(Exception error) => results.Enqueue(error);

        public Task<ImmutableArray<ContainerInfo>> ListAsync(CancellationToken token)
        {
            Calls++;
            var next = results.Dequeue();
            if (next is Exception error)
                throw error;
            return Task.FromResult((ImmutableArray<ContainerInfo>)next);
        }
    }

    private sealed class FakeProbe : IPortProbe
    {
        public int Calls { get; private set; }
        public Func<Task>? Wait { get; set; }
        public Exception? Error { get; set; }

        public async Task<ushort> ProbeAsync(string host, IReadOnlyList<ushort> ports, TimeSpan timeout, CancellationToken token)
        {
            Calls++;
            if (Wait is not null)
                await Wait();
            if (Error is not null)
                throw Error;
            return ports[0];
        }
    }
}
