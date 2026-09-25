using System.Collections.Immutable;
using DevLocal.Core;
using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class DiscoveryRunnerTests
{
    [Fact]
    public async Task EventRefreshPublishesCompleteLatestSnapshotAndClosesOnStop()
    {
        using var cancellation = new CancellationTokenSource();
        var source = new FakeSource { Current = [Container("first")] };
        var events = new FakeEvents();
        var state = new DiscoveryState(source, TimeSpan.FromHours(1), TimeSpan.FromSeconds(2));
        var runner = new DiscoveryRunner(state, events, TimeSpan.Zero);
        var running = runner.RunAsync(cancellation.Token);

        var first = await runner.Updates.ReadAsync(cancellation.Token).AsTask().WaitAsync(TimeSpan.FromSeconds(5));
        Assert.Equal("first", first.Snapshot[0].ContainerName);
        await events.Ready.Task.WaitAsync(TimeSpan.FromSeconds(5));
        source.Current = [Container("second")];
        events.Notify();
        await WaitUntilAsync(() => source.Calls >= 2);
        source.Current = [Container("third")];
        events.Notify();
        await WaitUntilAsync(() => runner.Updates.TryPeek(out var pending) &&
            pending.Snapshot[0].ContainerName == "third");

        var latest = await runner.Updates.ReadAsync(cancellation.Token).AsTask().WaitAsync(TimeSpan.FromSeconds(5));
        Assert.Equal("third", latest.Snapshot[0].ContainerName);
        cancellation.Cancel();
        await running.WaitAsync(TimeSpan.FromSeconds(5));
        Assert.True(runner.Updates.Completion.IsCompleted);
    }

    [Fact]
    public async Task PollBackstopsMissedEvents()
    {
        using var cancellation = new CancellationTokenSource();
        var source = new FakeSource { Current = [Container("first")] };
        var state = new DiscoveryState(source, TimeSpan.FromHours(1), TimeSpan.FromSeconds(2));
        var runner = new DiscoveryRunner(state, new FakeEvents(), TimeSpan.FromMilliseconds(40));
        var running = runner.RunAsync(cancellation.Token);

        var first = await runner.Updates.ReadAsync(cancellation.Token).AsTask().WaitAsync(TimeSpan.FromSeconds(5));
        Assert.Equal("first", first.Snapshot[0].ContainerName);
        source.Current = [Container("polled")];
        await WaitUntilAsync(() => runner.Updates.TryPeek(out var pending) &&
            pending.Snapshot[0].ContainerName == "polled");
        var updated = await runner.Updates.ReadAsync(cancellation.Token).AsTask().WaitAsync(TimeSpan.FromSeconds(5));

        Assert.Equal("polled", updated.Snapshot[0].ContainerName);
        cancellation.Cancel();
        await running.WaitAsync(TimeSpan.FromSeconds(5));
    }

    private static async Task WaitUntilAsync(Func<bool> ready)
    {
        for (var attempt = 0; attempt < 100 && !ready(); attempt++)
            await Task.Delay(20);
        Assert.True(ready());
    }

    private static ContainerInfo Container(string name) => new()
    {
        ContainerId = name,
        ContainerName = name,
        IsRunning = true
    };

    private sealed class FakeSource : IContainerSnapshotSource
    {
        private int calls;
        public ImmutableArray<ContainerInfo> Current { get; set; } = [];
        public int Calls => Volatile.Read(ref calls);
        public Task<ImmutableArray<ContainerInfo>> ListAsync(CancellationToken token)
        {
            Interlocked.Increment(ref calls);
            return Task.FromResult(Current);
        }
    }

    private sealed class FakeEvents : IDockerEventSource
    {
        private Action? onRefresh;
        public TaskCompletionSource Ready { get; } = new(TaskCreationOptions.RunContinuationsAsynchronously);
        public async Task WatchAsync(Action callback, CancellationToken token)
        {
            onRefresh = callback;
            Ready.TrySetResult();
            await Task.Delay(Timeout.InfiniteTimeSpan, token);
        }
        public void Notify() => onRefresh?.Invoke();
    }
}
