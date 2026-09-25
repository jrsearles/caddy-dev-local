using DevLocal.Core;
using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class UiHookTests
{
    [Fact]
    public async Task WritesStableArtifactsAndRegistersIndexRoute()
    {
        using var temp = new TempDirectory();
        var caddy = new FakeCaddy { Config = "{\"apps\":{\"http\":{}}}" };
        var hook = new UiHook("dev.local", temp.Path, caddy);
        var state = new DiscoveryStatus(DateTimeOffset.FromUnixTimeSeconds(1700000000), "");
        var web = new ContainerInfo { ContainerId = "a", ContainerName = "web", IsRunning = true, Ports = [80], SelectedPort = 80 };
        var api = new ContainerInfo { ContainerId = "b", ContainerName = "api", IsRunning = true, Ports = [443], SelectedPort = 443 };
        await hook.ApplyAsync(new DiscoveryUpdate([api, web], state), CancellationToken.None);
        var version = await File.ReadAllTextAsync(System.IO.Path.Combine(temp.Path, "version.json"));
        var indexPath = System.IO.Path.Combine(temp.Path, "index.html");
        var firstWrite = File.GetLastWriteTimeUtc(indexPath);

        await hook.ApplyAsync(new DiscoveryUpdate([web, api], state), CancellationToken.None);

        Assert.Equal(version, await File.ReadAllTextAsync(System.IO.Path.Combine(temp.Path, "version.json")));
        Assert.Equal(firstWrite, File.GetLastWriteTimeUtc(indexPath));
        Assert.Contains("\"apps\":{\"http\":{}}", await File.ReadAllTextAsync(indexPath));
        Assert.Contains(".card", await File.ReadAllTextAsync(System.IO.Path.Combine(temp.Path, "index.css")));
        Assert.Contains("devlocal-index", caddy.Desired!.Routes.Keys);
        Assert.Contains("devlocal-tls-ui", caddy.Desired.Policies.Keys);
        Assert.Equal(temp.Path, caddy.Desired.Routes["devlocal-index"]
            .GetProperty("handle")[0].GetProperty("routes")[0].GetProperty("handle")[0]
            .GetProperty("root").GetString());
    }

    [Fact]
    public async Task KeepsLastGoodCaddyConfigWhenAdminUnavailableAndCleansOnlyGeneratedFiles()
    {
        using var temp = new TempDirectory();
        var caddy = new FakeCaddy { Config = "{\"cached\":true}" };
        var hook = new UiHook("dev.local", temp.Path, caddy);
        var update = new DiscoveryUpdate([], new DiscoveryStatus(null, ""));
        await hook.ApplyAsync(update, CancellationToken.None);
        caddy.Error = new IOException("admin is restarting");

        await hook.ApplyAsync(update, CancellationToken.None);

        Assert.Contains("\"cached\":true", await File.ReadAllTextAsync(System.IO.Path.Combine(temp.Path, "index.html")));
        await File.WriteAllTextAsync(System.IO.Path.Combine(temp.Path, "keep.txt"), "keep");
        await hook.CleanupAsync(CancellationToken.None);
        Assert.True(caddy.Cleaned);
        Assert.False(File.Exists(System.IO.Path.Combine(temp.Path, "index.html")));
        Assert.False(File.Exists(System.IO.Path.Combine(temp.Path, "index.css")));
        Assert.False(File.Exists(System.IO.Path.Combine(temp.Path, "version.json")));
        Assert.Equal("keep", await File.ReadAllTextAsync(System.IO.Path.Combine(temp.Path, "keep.txt")));
    }

    [Fact]
    public async Task DoesNotClaimIndexHostReservedByCustomContainerDomain()
    {
        using var temp = new TempDirectory();
        var caddy = new FakeCaddy();
        var hook = new UiHook("dev.local", temp.Path, caddy);
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

    private sealed class TempDirectory : IDisposable
    {
        public string Path { get; } = System.IO.Path.Combine(System.IO.Path.GetTempPath(), Guid.NewGuid().ToString());
        public TempDirectory() => Directory.CreateDirectory(Path);
        public void Dispose() => Directory.Delete(Path, recursive: true);
    }

    private sealed class FakeCaddy : ICaddyConfigClient
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
