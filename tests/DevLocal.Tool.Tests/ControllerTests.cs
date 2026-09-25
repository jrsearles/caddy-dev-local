using System.Collections.Immutable;
using System.Text.Json;
using DevLocal.Core;
using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class ControllerTests
{
    [Fact]
    public async Task OnePassReconcilesCaddyAndHostsFromSameEnrichedSnapshot()
    {
        var temp = Path.Combine(Path.GetTempPath(), Guid.NewGuid().ToString());
        Directory.CreateDirectory(temp);
        var hostsPath = Path.Combine(temp, "hosts");
        try
        {
            var source = new FakeSource
            {
                Snapshot = [new ContainerInfo
                {
                    ContainerId = "web",
                    ContainerName = "web",
                    IsRunning = true,
                    Ports = [80],
                    PublishedPorts = ImmutableDictionary<ushort, ushort>.Empty.Add(80, 8080)
                }]
            };
            var caddy = new FakeCaddy();
            var runtime = new HookRuntime();
            runtime.Register(new CaddyHook(caddy, "dev.local", tracing: true));
            runtime.Register(new HostsHook(new HostsFileReconciler(hostsPath), "dev.local", true, true));
            var controller = new Controller(
                new DiscoveryState(source, TimeSpan.FromHours(1), TimeSpan.FromSeconds(2), new FakeProbe()), runtime);

            await controller.RunOnceAsync(CancellationToken.None);

            Assert.Equal("localhost:8080", caddy.Desired!.Routes["devlocal-route-web-dev-local"]
                .GetProperty("handle")[1].GetProperty("routes")[0].GetProperty("handle")[0]
                .GetProperty("upstreams")[0].GetProperty("dial").GetString());
            Assert.Contains("127.0.0.1    web.localhost", await File.ReadAllTextAsync(hostsPath));

            await controller.CleanupAsync(CancellationToken.None);
            Assert.True(caddy.Cleaned);
            Assert.Equal("", await File.ReadAllTextAsync(hostsPath));
        }
        finally
        {
            Directory.Delete(temp, true);
        }
    }

    [Fact]
    public async Task OnePassFailureDoesNotTouchHooks()
    {
        var source = new FakeSource { Error = new IOException("Docker unavailable") };
        var caddy = new FakeCaddy();
        var runtime = new HookRuntime();
        runtime.Register(new CaddyHook(caddy, "dev.local", tracing: true));
        var controller = new Controller(new DiscoveryState(source, TimeSpan.FromHours(1), TimeSpan.FromSeconds(2)), runtime);

        await Assert.ThrowsAsync<InvalidOperationException>(() => controller.RunOnceAsync(CancellationToken.None));

        Assert.Null(caddy.Desired);
    }

    [Fact]
    public async Task ContinuousRunAppliesInitialThenEventSnapshotsAndStopsOnCancellation()
    {
        using var cancellation = new CancellationTokenSource();
        var source = new FakeSource { Snapshot = [new ContainerInfo { ContainerId = "initial", ContainerName = "initial", IsRunning = true }] };
        var events = new FakeEvents();
        var discovery = new DiscoveryState(source, TimeSpan.FromHours(1), TimeSpan.FromSeconds(2));
        var runner = new DiscoveryRunner(discovery, events, TimeSpan.Zero);
        var initial = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var changed = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var runtime = new HookRuntime();
        runtime.Register(new FakeHook((update, _) =>
        {
            if (update.Snapshot[0].ContainerId == "initial") initial.TrySetResult();
            if (update.Snapshot[0].ContainerId == "changed") changed.TrySetResult();
            return Task.CompletedTask;
        }));
        var controller = new Controller(discovery, runtime, runner);
        var running = controller.RunAsync(cancellation.Token);

        await initial.Task.WaitAsync(TimeSpan.FromSeconds(5));
        source.Snapshot = [new ContainerInfo { ContainerId = "changed", ContainerName = "changed", IsRunning = true }];
        events.Trigger();
        await changed.Task.WaitAsync(TimeSpan.FromSeconds(5));
        cancellation.Cancel();

        await running.WaitAsync(TimeSpan.FromSeconds(5));
    }

    private sealed class FakeSource : IContainerSnapshotSource
    {
        public ImmutableArray<ContainerInfo> Snapshot { get; set; } = [];
        public Exception? Error { get; set; }
        public Task<ImmutableArray<ContainerInfo>> ListAsync(CancellationToken token) =>
            Error is null ? Task.FromResult(Snapshot) : Task.FromException<ImmutableArray<ContainerInfo>>(Error);
    }

    private sealed class FakeProbe : IPortProbe
    {
        public Task<ushort> ProbeAsync(string host, IReadOnlyList<ushort> ports, TimeSpan timeout, CancellationToken token) =>
            Task.FromResult(ports[0]);
    }

    private sealed class FakeEvents : IDockerEventSource
    {
        private Action? onRefresh;
        public async Task WatchAsync(Action callback, CancellationToken token)
        {
            onRefresh = callback;
            await Task.Delay(Timeout.InfiniteTimeSpan, token);
        }
        public void Trigger() => onRefresh?.Invoke();
    }

    private sealed class FakeHook(Func<DiscoveryUpdate, CancellationToken, Task> apply) : IUpdateHook
    {
        public string Name => "fake";
        public Task ApplyAsync(DiscoveryUpdate update, CancellationToken token) => apply(update, token);
        public Task CleanupAsync(CancellationToken token) => Task.CompletedTask;
    }

    private sealed class FakeCaddy : ICaddyConfigClient
    {
        public CaddyResources? Desired { get; private set; }
        public bool Cleaned { get; private set; }
        public Task ReconcileAsync(CaddyResources desired, Predicate<string> owned, CancellationToken token)
        {
            Desired = desired;
            return Task.CompletedTask;
        }
        public Task CleanupAsync(CancellationToken token)
        {
            Cleaned = true;
            return Task.CompletedTask;
        }
        public Task<string> RunningConfigAsync(CancellationToken token) => Task.FromResult("{}");
    }
}
