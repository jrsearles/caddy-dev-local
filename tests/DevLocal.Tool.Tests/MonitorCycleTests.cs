using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class MonitorCycleTests
{
    [Fact]
    public async Task ReportsIndependentDependenciesAndRecoversOnNextCheck()
    {
        var probe = new FakeProbe { DockerError = new IOException("Docker Desktop is not running") };
        var settings = ServiceSettings.Default;

        var first = await MonitorCycle.CheckAsync(probe, settings, CancellationToken.None);
        Assert.False(first.Docker.Connected);
        Assert.Contains("not running", first.Docker.Error);
        Assert.True(first.Caddy.Connected);
        Assert.Equal("docker_engine", probe.Pipe);
        Assert.Equal("http://localhost:2019", probe.AdminUrl);

        probe.DockerError = null;
        probe.CaddyError = new HttpRequestException("Caddy is restarting");
        var second = await MonitorCycle.CheckAsync(probe, settings, CancellationToken.None);
        Assert.True(second.Docker.Connected);
        Assert.False(second.Caddy.Connected);
        Assert.Contains("restarting", second.Caddy.Error);
    }

    [Fact]
    public async Task CancellationIsNotTreatedAsConnectionFailure()
    {
        using var cancellation = new CancellationTokenSource();
        cancellation.Cancel();
        var probe = new FakeProbe { DockerError = new OperationCanceledException() };

        await Assert.ThrowsAnyAsync<OperationCanceledException>(() =>
            MonitorCycle.CheckAsync(probe, ServiceSettings.Default, cancellation.Token));
    }

    [Fact]
    public void ServiceSettingsRejectInvalidEndpoints()
    {
        Assert.Throws<ArgumentException>(() => ServiceManager.ParseSettings(["--docker-pipe", @"\\.\pipe\docker_engine"]));
        Assert.Throws<ArgumentException>(() => ServiceManager.ParseSettings(["--caddy-admin", "invalid"]));
        Assert.Throws<ArgumentException>(() => ServiceManager.ParseSettings(["--unknown", "value"]));
        Assert.Equal("dockerDesktopLinuxEngine", ServiceManager.ParseSettings(["--docker-pipe", "dockerDesktopLinuxEngine"]).DockerPipe);
    }

    [Fact]
    public void SelectsNamedPipeFromDockerDesktopContext()
    {
        const string json = """
            [{"Endpoints":{"docker":{"Host":"npipe:////./pipe/dockerDesktopLinuxEngine"}}}]
            """;
        Assert.Equal("dockerDesktopLinuxEngine", DockerContext.PipeFromInspect(json));
        Assert.Null(DockerContext.PipeFromInspect("[{\"Endpoints\":{\"docker\":{\"Host\":\"ssh://host\"}}}]"));
    }

    [Fact]
    public void DeploymentPreservesPlatformSpecificRuntimeAssets()
    {
        var source = Path.Combine(Path.GetTempPath(), Guid.NewGuid().ToString());
        var destination = Path.Combine(Path.GetTempPath(), Guid.NewGuid().ToString());
        var relative = Path.Combine("runtimes", "win", "lib", "net10.0", "System.Diagnostics.EventLog.dll");
        try
        {
            Directory.CreateDirectory(Path.GetDirectoryName(Path.Combine(source, relative))!);
            File.WriteAllText(Path.Combine(source, relative), "platform-specific assembly");
            File.WriteAllText(Path.Combine(source, "DevLocal.Tool.runtimeconfig.json"), "{}");

            ServiceManager.CopyRuntimeFiles(source, destination);

            Assert.Equal("platform-specific assembly", File.ReadAllText(Path.Combine(destination, relative)));
            Assert.True(File.Exists(Path.Combine(destination, "DevLocal.Tool.runtimeconfig.json")));
        }
        finally
        {
            if (Directory.Exists(source)) Directory.Delete(source, true);
            if (Directory.Exists(destination)) Directory.Delete(destination, true);
        }
    }

    private sealed class FakeProbe : IDependencyProbe
    {
        public Exception? DockerError { get; set; }
        public Exception? CaddyError { get; set; }
        public string? Pipe { get; private set; }
        public string? AdminUrl { get; private set; }

        public Task CheckDockerAsync(string pipe, CancellationToken token)
        {
            Pipe = pipe;
            if (DockerError is not null)
                throw DockerError;
            return Task.CompletedTask;
        }

        public Task CheckCaddyAsync(string adminUrl, CancellationToken token)
        {
            AdminUrl = adminUrl;
            if (CaddyError is not null)
                throw CaddyError;
            return Task.CompletedTask;
        }
    }
}
