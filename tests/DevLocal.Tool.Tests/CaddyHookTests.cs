using System.Collections.Immutable;
using DevLocal.Core;
using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class CaddyHookTests
{
    [Fact]
    public async Task DoesNotDeleteRoutesOnInitialDiscoveryFailure()
    {
        var client = new FakeClient();
        var hook = new CaddyHook(client, "dev.local", tracing: true);

        await hook.ApplyAsync(new DiscoveryUpdate([], new DiscoveryStatus(null, "Docker is unavailable")), CancellationToken.None);

        Assert.Null(client.Desired);
    }

    [Fact]
    public async Task ReconcilesOnlyRunningPublishedContainers()
    {
        var client = new FakeClient();
        var hook = new CaddyHook(client, "dev.local", tracing: true);
        var update = new DiscoveryUpdate(
        [
            new ContainerInfo { ContainerId = "web", ContainerName = "web", IsRunning = true, SelectedPort = 8080 },
            new ContainerInfo { ContainerId = "stopped", ContainerName = "stopped" }
        ], new DiscoveryStatus(DateTimeOffset.UtcNow, ""));

        await hook.ApplyAsync(update, CancellationToken.None);

        Assert.Contains("devlocal-route-web-dev-local", client.Desired!.Routes.Keys);
        Assert.DoesNotContain("devlocal-route-stopped-dev-local", client.Desired.Routes.Keys);
        Assert.True(client.Owns!("devlocal-route-web-dev-local"));
        Assert.False(client.Owns("user-route"));
    }

    private sealed class FakeClient : ICaddyConfigClient
    {
        public CaddyResources? Desired { get; private set; }
        public Predicate<string>? Owns { get; private set; }
        public Task ReconcileAsync(CaddyResources desired, Predicate<string> owned, CancellationToken token)
        {
            Desired = desired;
            Owns = owned;
            return Task.CompletedTask;
        }
        public Task CleanupAsync(CancellationToken token) => Task.CompletedTask;
        public Task<string> RunningConfigAsync(CancellationToken token) => Task.FromResult("{}");
    }
}
