using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class ServiceManagerTests
{
    [Fact]
    public void ServiceSettingsRejectInvalidEndpoints()
    {
        Assert.Throws<ArgumentException>(() => ServiceManager.ParseSettings(["--caddy-admin", "invalid"]));
        Assert.Throws<ArgumentException>(() => ServiceManager.ParseSettings(["--unknown", "value"]));
        Assert.Equal("http://localhost:2019", ServiceManager.ParseSettings([]).CaddyAdmin);
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

    [Fact]
    public void DeploymentCopiesStaticWebAssets()
    {
        var source = Path.Combine(Path.GetTempPath(), Guid.NewGuid().ToString());
        var destination = Path.Combine(Path.GetTempPath(), Guid.NewGuid().ToString());
        var script = Path.Combine("wwwroot", "_framework", "blazor.web.js");
        try
        {
            Directory.CreateDirectory(Path.GetDirectoryName(Path.Combine(source, script))!);
            File.WriteAllText(Path.Combine(source, script), "blazor runtime");
            File.WriteAllText(Path.Combine(source, "DevLocal.Tool.runtimeconfig.json"), "{}");

            ServiceManager.CopyRuntimeFiles(source, destination);

            Assert.Equal("blazor runtime", File.ReadAllText(Path.Combine(destination, script)));
        }
        finally
        {
            if (Directory.Exists(source)) Directory.Delete(source, true);
            if (Directory.Exists(destination)) Directory.Delete(destination, true);
        }
    }
}
