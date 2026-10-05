using DevLocal.Core;
using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class UiServerHookTests
{
    [Fact]
    public async Task ProxiesIndexHostToRunningSiteAndKeepsTlsPolicy()
    {
        var caddy = new FakeCaddy { Config = "{\"apps\":{\"http\":{}}}" };
        await using var hook = new UiServerHook("dev.local", 0, caddy);
        var state = new DiscoveryStatus(DateTimeOffset.FromUnixTimeSeconds(1700000000), "");
        var web = new ContainerInfo { ContainerId = "a", ContainerName = "web", IsRunning = true, Ports = [80], SelectedPort = 80 };
        var api = new ContainerInfo { ContainerId = "b", ContainerName = "api", IsRunning = true, Ports = [443], SelectedPort = 443 };

        await hook.ApplyAsync(new DiscoveryUpdate([web, api], state), CancellationToken.None);

        var route = caddy.Desired!.Routes["devlocal-index"];
        var handler = route.GetProperty("handle")[0];
        Assert.Equal("reverse_proxy", handler.GetProperty("handler").GetString());
        Assert.Equal(hook.BaseUrl.Substring("http://".Length), Dial(caddy.Desired!));
        Assert.Equal(new[] { "dev.local", "dev.localhost" },
            route.GetProperty("match")[0].GetProperty("host").EnumerateArray().Select(item => item.GetString()).ToArray());
        Assert.Contains("devlocal-tls-ui", caddy.Desired.Policies.Keys);
        Assert.Contains("\"apps\":{\"http\":{}}", hook.State.Current.ConfigJson);
        Assert.Equal(2, hook.State.Current.Containers.Length);
    }

    [Fact]
    public async Task ReusesTheSameUpstreamAcrossRefreshes()
    {
        var caddy = new FakeCaddy();
        await using var hook = new UiServerHook("dev.local", 0, caddy);
        var update = new DiscoveryUpdate([], new DiscoveryStatus(DateTimeOffset.UtcNow, ""));

        await hook.ApplyAsync(update, CancellationToken.None);
        var first = Dial(caddy.Desired!);
        await hook.ApplyAsync(update, CancellationToken.None);

        Assert.Equal(first, Dial(caddy.Desired!));
        Assert.NotEqual(0, int.Parse(first.Split(':')[1], System.Globalization.CultureInfo.InvariantCulture));
    }

    [Fact]
    public async Task PinsTheRequestedPort()
    {
        using var port = new FreePort();
        var caddy = new FakeCaddy();
        await using var hook = new UiServerHook("dev.local", port.Value, caddy);
        var update = new DiscoveryUpdate([], new DiscoveryStatus(DateTimeOffset.UtcNow, ""));

        await hook.ApplyAsync(update, CancellationToken.None);

        Assert.Equal($"127.0.0.1:{port.Value}", Dial(caddy.Desired!));
    }

    [Fact]
    public async Task KeepsLastGoodCaddyConfigWhenAdminUnavailableAndCleansRoutes()
    {
        var caddy = new FakeCaddy { Config = "{\"cached\":true}" };
        await using var hook = new UiServerHook("dev.local", 0, caddy);
        var update = new DiscoveryUpdate([], new DiscoveryStatus(null, ""));
        await hook.ApplyAsync(update, CancellationToken.None);
        caddy.Error = new IOException("admin is restarting");

        await hook.ApplyAsync(update, CancellationToken.None);
        await hook.CleanupAsync(CancellationToken.None);

        Assert.Contains("\"cached\":true", hook.State.Current.ConfigJson);
        Assert.True(caddy.Cleaned);
    }

    [Fact]
    public async Task DoesNotClaimIndexHostReservedByCustomContainerDomain()
    {
        var caddy = new FakeCaddy();
        await using var hook = new UiServerHook("dev.local", 0, caddy);
        var update = new DiscoveryUpdate(
            [new ContainerInfo
            {
                ContainerId = "custom", ContainerName = "custom", IsRunning = true,
                CustomDomains = [new CustomDomain(8080, "dev.local")]
            }], new DiscoveryStatus(DateTimeOffset.UtcNow, ""));

        await hook.ApplyAsync(update, CancellationToken.None);

        var subjects = caddy.Desired!.Policies["devlocal-tls-ui"].GetProperty("subjects");
        Assert.Equal(new[] { "dev.localhost" }, subjects.EnumerateArray().Select(item => item.GetString()!).ToArray());
    }

    private static string Dial(CaddyResources desired) =>
        desired.Routes["devlocal-index"].GetProperty("handle")[0]
            .GetProperty("upstreams")[0].GetProperty("dial").GetString()!;

    private sealed class FreePort : IDisposable
    {
        private readonly System.Net.Sockets.TcpListener listener = new(System.Net.IPAddress.Loopback, 0);

        public FreePort()
        {
            listener.Start();
            Value = ((System.Net.IPEndPoint)listener.LocalEndpoint).Port;
            listener.Stop();
        }

        public int Value { get; }

        public void Dispose() { }
    }

    internal sealed class FakeCaddy : ICaddyConfigClient
    {
        public string Config { get; set; } = "";
        public Exception? Error { get; set; }
        public CaddyResources? Desired { get; private set; }
        public bool Cleaned { get; private set; }

        public Task<string> RunningConfigAsync(CancellationToken token) =>
            Error is null ? Task.FromResult(Config) : Task.FromException<string>(Error);

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
    }
}