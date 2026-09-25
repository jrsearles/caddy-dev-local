using System.Text.Json;
using DevLocal.Core;
using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class HostsFileReconcilerTests
{
    public sealed record HostsCase(string Name, string Tld, string[] Domains, string ExpectedBlock);

    public static IEnumerable<object[]> Cases()
    {
        var path = Path.Combine(AppContext.BaseDirectory, "Fixtures", "hosts_cases.json");
        var cases = JsonSerializer.Deserialize<HostsCase[]>(File.ReadAllText(path),
            new JsonSerializerOptions { PropertyNameCaseInsensitive = true })!;
        return cases.Select(item => new object[] { item });
    }

    [Theory]
    [MemberData(nameof(Cases))]
    public void ManagedBlockMatchesGoFixture(HostsCase fixture) =>
        Assert.Equal(fixture.ExpectedBlock, HostsBlock.Build(fixture.Tld, fixture.Domains));

    [Fact]
    public async Task ReplacesOnlyOwnedBlockAndCleansItWithoutTouchingOtherEntries()
    {
        using var temp = new TempHosts();
        const string original = "10.1.2.3 existing.local\n# dev-local:BEGIN\n127.0.0.1    old.dev.local\n# dev-local:END\n10.1.2.4 trailing.local\n";
        await File.WriteAllTextAsync(temp.Path, original);

        await temp.Reconciler.SyncAsync("dev.local", ["web.dev.local"], CancellationToken.None);
        var updated = await File.ReadAllTextAsync(temp.Path);
        Assert.Contains("10.1.2.3 existing.local", updated);
        Assert.Contains("10.1.2.4 trailing.local", updated);
        Assert.Contains("127.0.0.1    web.dev.local", updated);
        Assert.DoesNotContain("old.dev.local", updated);

        await temp.Reconciler.RemoveAsync(CancellationToken.None);
        Assert.Equal("10.1.2.3 existing.local\n10.1.2.4 trailing.local\n", await File.ReadAllTextAsync(temp.Path));
    }

    [Fact]
    public async Task WritesNoChangesWhenManagedBlockAlreadyMatches()
    {
        using var temp = new TempHosts();
        await temp.Reconciler.SyncAsync("dev.local", ["web.dev.local"], CancellationToken.None);
        var before = await File.ReadAllTextAsync(temp.Path);
        var old = new DateTime(2020, 1, 1, 0, 0, 0, DateTimeKind.Utc);
        File.SetLastWriteTimeUtc(temp.Path, old);

        await temp.Reconciler.SyncAsync("dev.local", ["web.dev.local"], CancellationToken.None);

        Assert.Equal(before, await File.ReadAllTextAsync(temp.Path));
        Assert.Equal(old, File.GetLastWriteTimeUtc(temp.Path));
    }

    [Fact]
    public async Task RemoveDoesNotCreateOrRewriteUnmanagedFile()
    {
        using var temp = new TempHosts();
        await temp.Reconciler.RemoveAsync(CancellationToken.None);
        Assert.False(File.Exists(temp.Path));
        await File.WriteAllTextAsync(temp.Path, "127.0.0.1 localhost\n");

        await temp.Reconciler.RemoveAsync(CancellationToken.None);

        Assert.Equal("127.0.0.1 localhost\n", await File.ReadAllTextAsync(temp.Path));
    }

    private sealed class TempHosts : IDisposable
    {
        private readonly string directory = System.IO.Path.Combine(System.IO.Path.GetTempPath(), Guid.NewGuid().ToString());
        public string Path => System.IO.Path.Combine(directory, "hosts");
        public HostsFileReconciler Reconciler => new(Path);

        public TempHosts() => Directory.CreateDirectory(directory);
        public void Dispose() => Directory.Delete(directory, true);
    }
}
