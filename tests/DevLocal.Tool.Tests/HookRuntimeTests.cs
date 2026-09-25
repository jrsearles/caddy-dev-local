using DevLocal.Core;
using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class HookRuntimeTests
{
    [Fact]
    public async Task ApplyOnceContinuesPastFailureAndSequenceStopsAtFailedChild()
    {
        var called = new List<string>();
        var first = new FakeHook("first", apply: (_, _) =>
        {
            called.Add("first");
            throw new InvalidOperationException("failed");
        });
        var skipped = new FakeHook("skipped", apply: (_, _) =>
        {
            called.Add("skipped");
            return Task.CompletedTask;
        });
        var independent = new FakeHook("independent", apply: (_, _) =>
        {
            called.Add("independent");
            return Task.CompletedTask;
        });
        var runtime = new HookRuntime();
        runtime.Register(new HookSequence("ordered", first, skipped));
        runtime.Register(independent);

        await Assert.ThrowsAsync<AggregateException>(() => runtime.ApplyOnceAsync(Update(0), CancellationToken.None));

        Assert.Equal(["first", "independent"], called);
    }

    [Fact]
    public async Task IndependentWorkersCoalescePendingSnapshotsToLatest()
    {
        using var cancellation = new CancellationTokenSource();
        var slowStarted = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var release = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var slowLatest = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var fastLatest = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var slow = new FakeHook("slow", apply: async (update, _) =>
        {
            if (update.Snapshot[0].ContainerId == "1")
            {
                slowStarted.SetResult();
                await release.Task;
            }
            if (update.Snapshot[0].ContainerId == "4")
                slowLatest.SetResult();
        });
        var fast = new FakeHook("fast", apply: (update, _) =>
        {
            if (update.Snapshot[0].ContainerId == "4")
                fastLatest.SetResult();
            return Task.CompletedTask;
        });
        var runtime = new HookRuntime();
        runtime.Register(slow);
        runtime.Register(fast);
        await runtime.StartAsync(Update(0), cancellation.Token);

        runtime.Submit(Update(1));
        await slowStarted.Task.WaitAsync(TimeSpan.FromSeconds(5));
        runtime.Submit(Update(2));
        runtime.Submit(Update(3));
        runtime.Submit(Update(4));
        await fastLatest.Task.WaitAsync(TimeSpan.FromSeconds(5));
        release.SetResult();
        await slowLatest.Task.WaitAsync(TimeSpan.FromSeconds(5));
        cancellation.Cancel();
        await runtime.WaitAsync();

        Assert.Equal(["0", "1", "4"], slow.Calls);
        Assert.Contains("4", fast.Calls);
    }

    [Fact]
    public async Task CleanupAttemptsAllHooksEvenWhenOneFails()
    {
        var cleaned = new List<string>();
        var first = new FakeHook("first", cleanup: _ =>
        {
            cleaned.Add("first");
            throw new IOException("cleanup failed");
        });
        var second = new FakeHook("second", cleanup: _ =>
        {
            cleaned.Add("second");
            return Task.CompletedTask;
        });
        var runtime = new HookRuntime();
        runtime.Register(first);
        runtime.Register(second);

        await Assert.ThrowsAsync<AggregateException>(() => runtime.CleanupAsync(CancellationToken.None));

        Assert.Equal(["first", "second"], cleaned);
    }

    [Fact]
    public async Task WorkerReportsFailureAndContinuesWithNextSnapshot()
    {
        using var cancellation = new CancellationTokenSource();
        var error = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var recovered = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var runtime = new HookRuntime((name, exception) =>
        {
            Assert.Equal("faulty", name);
            Assert.Equal("temporary", exception.Message);
            error.TrySetResult();
        });
        runtime.Register(new FakeHook("faulty", apply: (update, _) =>
        {
            if (update.Snapshot[0].ContainerId == "1")
                throw new IOException("temporary");
            if (update.Snapshot[0].ContainerId == "2")
                recovered.TrySetResult();
            return Task.CompletedTask;
        }));
        await runtime.StartAsync(Update(0), cancellation.Token);

        runtime.Submit(Update(1));
        await error.Task.WaitAsync(TimeSpan.FromSeconds(5));
        runtime.Submit(Update(2));
        await recovered.Task.WaitAsync(TimeSpan.FromSeconds(5));
        cancellation.Cancel();
        await runtime.WaitAsync();
    }

    private static DiscoveryUpdate Update(int id) => new(
        [new ContainerInfo { ContainerId = id.ToString() }], new DiscoveryStatus(DateTimeOffset.UtcNow, ""));

    private sealed class FakeHook(
        string name,
        Func<DiscoveryUpdate, CancellationToken, Task>? apply = null,
        Func<CancellationToken, Task>? cleanup = null) : IUpdateHook
    {
        private readonly List<string> calls = [];
        public string Name => name;
        public string[] Calls
        {
            get { lock (calls) return calls.ToArray(); }
        }
        public async Task ApplyAsync(DiscoveryUpdate update, CancellationToken token)
        {
            lock (calls) calls.Add(update.Snapshot[0].ContainerId);
            if (apply is not null)
                await apply(update, token);
        }
        public Task CleanupAsync(CancellationToken token) => cleanup?.Invoke(token) ?? Task.CompletedTask;
    }
}
