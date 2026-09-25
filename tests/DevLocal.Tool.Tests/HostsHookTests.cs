using System.Collections.Immutable;
using DevLocal.Core;
using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class HostsHookTests
{
    [Fact]
    public async Task UsesLatestAuthoritativeDomainsAndRemovesOnlyOwnedEntries()
    {
        var directory = Path.Combine(Path.GetTempPath(), Guid.NewGuid().ToString());
        Directory.CreateDirectory(directory);
        var path = Path.Combine(directory, "hosts");
        try
        {
            await File.WriteAllTextAsync(path, "127.0.0.1 unrelated.local\n");
            var hook = new HostsHook(new HostsFileReconciler(path), "dev.local", true, true);
            var first = new DiscoveryUpdate(
                [new ContainerInfo { ContainerName = "web", IsRunning = true, PublishedPorts = ImmutableDictionary<ushort, ushort>.Empty.Add(80, 8080) }],
                new DiscoveryStatus(DateTimeOffset.UtcNow, ""));
            await hook.ApplyAsync(first, CancellationToken.None);
            Assert.Contains("web.localhost", await File.ReadAllTextAsync(path));

            var stopped = first with { Snapshot = [first.Snapshot[0] with { IsRunning = false }] };
            await hook.ApplyAsync(stopped, CancellationToken.None);
            var content = await File.ReadAllTextAsync(path);
            Assert.DoesNotContain("web.localhost", content);
            Assert.Contains("127.0.0.1 unrelated.local", content);

            await hook.CleanupAsync(CancellationToken.None);
            Assert.Equal("127.0.0.1 unrelated.local\n", await File.ReadAllTextAsync(path));
        }
        finally
        {
            Directory.Delete(directory, true);
        }
    }

    [Fact]
    public async Task InitialDockerFailureDoesNotReplaceExistingManagedBlock()
    {
        var directory = Path.Combine(Path.GetTempPath(), Guid.NewGuid().ToString());
        Directory.CreateDirectory(directory);
        var path = Path.Combine(directory, "hosts");
        try
        {
            var original = HostsBlock.Build("dev.local", ["web.dev.local"]);
            await File.WriteAllTextAsync(path, original);
            var hook = new HostsHook(new HostsFileReconciler(path), "dev.local", true, true);

            await hook.ApplyAsync(new DiscoveryUpdate([], new DiscoveryStatus(null, "Docker unavailable")), CancellationToken.None);

            Assert.Equal(original, await File.ReadAllTextAsync(path));
        }
        finally
        {
            Directory.Delete(directory, true);
        }
    }

    [Fact]
    public async Task DisabledOrUnwritableHookDoesNotCreateHostsFile()
    {
        var directory = Path.Combine(Path.GetTempPath(), Guid.NewGuid().ToString());
        Directory.CreateDirectory(directory);
        var path = Path.Combine(directory, "hosts");
        try
        {
            foreach (var hook in new[]
            {
                new HostsHook(new HostsFileReconciler(path), "dev.local", false, true),
                new HostsHook(new HostsFileReconciler(path), "dev.local", true, false)
            })
                await hook.ApplyAsync(new DiscoveryUpdate([], new DiscoveryStatus(DateTimeOffset.UtcNow, "")), CancellationToken.None);

            Assert.False(File.Exists(path));
        }
        finally
        {
            Directory.Delete(directory, true);
        }
    }
}
