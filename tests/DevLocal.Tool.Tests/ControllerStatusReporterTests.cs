using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class ControllerStatusReporterTests
{
    [Fact]
    public async Task CapturesUiBaseUrlAndWritesStatus()
    {
        using var dir = new TempDir();
        var reporter = new ControllerStatusReporter(dir.Status, dir.Log);

        reporter.Announce("Index UI served from http://127.0.0.1:51234");
        await reporter.WriteAsync(["caddy-ui", "hosts"], CancellationToken.None);

        Assert.Equal("http://127.0.0.1:51234", reporter.UiBaseUrl);
        var status = await Read(dir.Status);
        Assert.Equal("continuous controller", status.Mode);
        Assert.Equal("http://127.0.0.1:51234", status.UiBaseUrl);
        Assert.Equal(["caddy-ui", "hosts"], status.Hooks);
        Assert.Empty(status.Errors);
        Assert.Contains("Index UI served from http://127.0.0.1:51234", await File.ReadAllTextAsync(dir.Log));
    }

    [Fact]
    public async Task RecordsHookFailuresInStatusAndLog()
    {
        using var dir = new TempDir();
        var reporter = new ControllerStatusReporter(dir.Status, dir.Log);

        reporter.Record("hosts", new IOException("hosts file is not writable"));
        await reporter.WriteAsync(["hosts"], CancellationToken.None);

        var status = await Read(dir.Status);
        Assert.Equal(["hosts: hosts file is not writable"], status.Errors);
        Assert.Contains("hosts failed: hosts file is not writable", await File.ReadAllTextAsync(dir.Log));
    }

    [Fact]
    public async Task KeepsUnrelatedAnnouncementsOutOfTheUiField()
    {
        using var dir = new TempDir();
        var reporter = new ControllerStatusReporter(dir.Status, dir.Log);

        reporter.Announce("something else entirely");
        await reporter.WriteAsync(["ui"], CancellationToken.None);

        var status = await Read(dir.Status);
        Assert.Null(status.UiBaseUrl);
        Assert.Contains("something else entirely", await File.ReadAllTextAsync(dir.Log));
    }

    [Fact]
    public async Task RefreshesHeartbeatWithoutLosingErrors()
    {
        using var dir = new TempDir();
        var reporter = new ControllerStatusReporter(dir.Status, dir.Log);
        reporter.Record("ui", new IOException("boom"));

        await reporter.WriteAsync(["ui"], CancellationToken.None);
        var first = await Read(dir.Status);
        await reporter.WriteAsync(["ui"], CancellationToken.None);
        var second = await Read(dir.Status);

        Assert.Equal(first.Errors, second.Errors);
        Assert.True(second.UpdatedAt >= first.UpdatedAt);
    }

    private static async Task<ControllerStatus> Read(string path)
    {
        var json = await File.ReadAllTextAsync(path);
        return System.Text.Json.JsonSerializer.Deserialize<ControllerStatus>(json,
            new System.Text.Json.JsonSerializerOptions { PropertyNameCaseInsensitive = true })!;
    }

    private sealed class TempDir : IDisposable
    {
        private readonly string root = Path.Combine(Path.GetTempPath(), "devlocal-reporter-" + Guid.NewGuid().ToString("N"));

        public string Status => Path.Combine(root, "controller-status.json");

        public string Log => Path.Combine(root, "controller.log");

        public void Dispose()
        {
            if (Directory.Exists(root))
                Directory.Delete(root, true);
        }
    }
}