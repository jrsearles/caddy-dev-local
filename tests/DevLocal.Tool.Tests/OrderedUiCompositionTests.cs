using DevLocal.Core;
using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class OrderedUiCompositionTests
{
    [Fact]
    public async Task UIReadsConfigAfterContainerRoutesAreReconciled()
    {
        var caddy = new FakeCaddy();
        var runtime = new HookRuntime();
        runtime.Register(new HookSequence("caddy-ui",
            new CaddyHook(caddy, "dev.local", tracing: true),
            new UiServerHook("dev.local", 0, caddy)));
        var update = new DiscoveryUpdate([new ContainerInfo
        {
            ContainerId = "web", ContainerName = "web", IsRunning = true, Ports = [8080], SelectedPort = 8080
        }], new DiscoveryStatus(DateTimeOffset.UtcNow, ""));

        await runtime.ApplyOnceAsync(update, CancellationToken.None);
        await runtime.DisposeAsync();

        Assert.Equal(new[] { "caddy", "read-config", "ui" }, caddy.Events);
        Assert.Contains("devlocal-route-web-dev-local", caddy.DesiredRoutes[0]);
        Assert.Equal("devlocal-index", string.Join(',', caddy.DesiredRoutes[^1]));
    }

    private sealed class FakeCaddy : ICaddyConfigClient
    {
        public List<string> Events { get; } = [];

        public List<string> DesiredRoutes { get; } = [];

        public Task ReconcileAsync(CaddyResources desired, Predicate<string> owned, CancellationToken token)
        {
            Events.Add(owned("devlocal-index") ? "ui" : "caddy");
            DesiredRoutes.Add(string.Join(',', desired.Routes.Keys));
            return Task.CompletedTask;
        }

        public Task<string> RunningConfigAsync(CancellationToken token)
        {
            Events.Add("read-config");
            return Task.FromResult("{\"route\":\"devlocal-route-web-dev-local\"}");
        }

        public Task CleanupAsync(CancellationToken token) => Task.CompletedTask;
    }
}