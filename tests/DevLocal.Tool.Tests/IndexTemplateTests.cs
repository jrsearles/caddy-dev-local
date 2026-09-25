using DevLocal.Tool;
using DevLocal.Core;
using System.Collections.Immutable;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class IndexTemplateTests
{
    [Fact]
    public void ExistingGoTemplateRendersEmptyPage()
    {
        var page = IndexTemplate.Render(IndexViewModelBuilder.Build("dev.local",
            new DiscoveryUpdate([], new DiscoveryStatus(null, "")), ""));

        Assert.Contains("No containers registered.", page);
        Assert.Contains("devlocal — Container Index", page);
        Assert.Contains(".card", IndexTemplate.Css);
        Assert.DoesNotContain("id=\"tab-config\"", page);
        Assert.DoesNotContain("id=\"errorBanner\"", page);
    }

    [Fact]
    public void RendersComposeCardsUsingPublishedPortsAndRunningCaddyConfig()
    {
        var container = new ContainerInfo
        {
            ContainerId = "deadbeef12345678",
            ContainerName = "web",
            Image = "nginx:alpine",
            Project = "example",
            Service = "web",
            IsCompose = true,
            IsRunning = true,
            Ports = [80],
            PublishedPorts = ImmutableDictionary<ushort, ushort>.Empty.Add(80, 8080),
            SelectedPort = 8080,
            Health = "healthy",
            Created = DateTimeOffset.FromUnixTimeSeconds(1700000000)
        };
        var update = new DiscoveryUpdate([container], new DiscoveryStatus(DateTimeOffset.UtcNow, ""));

        var page = IndexTemplate.Render(IndexViewModelBuilder.Build("dev.local", update, "{\"apps\":{}}"));

        Assert.Contains("example.web.dev.local", page);
        Assert.Contains("example.web.localhost", page);
        Assert.Contains("localhost:8080", DomainGenerator.DomainTargets("dev.local", update.Snapshot)["example.web.dev.local"]);
        Assert.Contains("data-published=\"80:8080\"", page);
        Assert.Contains("docker-desktop://dashboard/logs?containerIds=deadbeef12345678", page);
        Assert.Contains("id=\"config-json\"", page);
        Assert.Contains("<div class=\"project-section\"", page);
        Assert.Contains("1 project</span>", page);
        Assert.Contains("1 service</span>", page);
        Assert.Contains("health-badge health-healthy", page);
    }

    [Fact]
    public void EscapesHostileContainerFieldsAndEmbeddedConfig()
    {
        const string hostile = "\"><script>alert('x')</script>";
        var info = new ContainerInfo
        {
            ContainerId = "evil",
            ContainerName = hostile,
            Image = hostile,
            IsRunning = true,
            Ports = [80],
            PublishedPorts = ImmutableDictionary<ushort, ushort>.Empty.Add(80, 8080),
            SelectedPort = 8080,
            Labels = ImmutableDictionary<string, string>.Empty.Add("dev.local.test", hostile)
        };
        var update = new DiscoveryUpdate([info], new DiscoveryStatus(DateTimeOffset.UtcNow, hostile));

        var page = IndexTemplate.Render(IndexViewModelBuilder.Build("dev.local", update,
            "{\"injected\":\"</script><script>evil</script>\"}"));

        Assert.DoesNotContain(hostile, page);
        Assert.DoesNotContain("</script><script>evil", page);
        Assert.Contains("&lt;script&gt;", page);
        Assert.Contains("\\u003C/script\\u003E", page, StringComparison.OrdinalIgnoreCase);
    }
}
