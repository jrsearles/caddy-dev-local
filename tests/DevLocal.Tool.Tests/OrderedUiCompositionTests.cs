using DevLocal.Core;
using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class OrderedUiCompositionTests
{
    [Fact]
    public async Task UIReadsConfigAfterContainerRoutesAreReconciled()
    {
        var directory = Path.Combine(Path.GetTempPath(), Guid.NewGuid().ToString());
        Directory.CreateDirectory(directory);
        try
        {
            var caddy = new FakeCaddy();
            var runtime = new HookRuntime();
            runtime.Register(new HookSequence("caddy-ui",
                new CaddyHook(caddy, "dev.local", tracing: true),
                new UiHook("dev.local", directory, caddy)));
            var update = new DiscoveryUpdate([new ContainerInfo
            {
                ContainerId = "web", ContainerName = "web", IsRunning = true, SelectedPort = 8080
            }], new DiscoveryStatus(DateTimeOffset.UtcNow, ""));

            await runtime.ApplyOnceAsync(update, CancellationToken.None);

            Assert.Equal(new[] { "caddy", "read-config", "ui" }, caddy.Events);
            var page = await File.ReadAllTextAsync(Path.Combine(directory, "index.html"));
            Assert.Contains("devlocal-route-web-dev-local", page);
        }
        finally
        {
            Directory.Delete(directory, true);
        }
    }

    private sealed class FakeCaddy : ICaddyConfigClient
    {
        public List<string> Events { get; } = [];
        public Task ReconcileAsync(CaddyResources desired, Predicate<string> owned, CancellationToken token)
        {
            Events.Add(owned("devlocal-index") ? "ui" : "caddy");
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
