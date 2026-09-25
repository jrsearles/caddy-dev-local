using DevLocal.Tool;
using Docker.DotNet.Models;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class DockerDiscoveryClientTests
{
    [Fact]
    public void MapsDockerSummaryToHostTargetsAndComposeMetadata()
    {
        var summary = new ContainerListResponse
        {
            ID = "1234567890abcdef",
            Names = ["/example-web"],
            Image = "nginx:alpine",
            State = "running",
            Ports = [new PortSummary { PrivatePort = 80, PublicPort = 8080 }, new PortSummary { PrivatePort = 443 }],
            Labels = new Dictionary<string, string>
            {
                ["com.docker.compose.project"] = "example",
                ["com.docker.compose.service"] = "web",
                ["dev.local.domains"] = "80:api.custom.local;bogus;443:secure.local"
            },
            Health = new HealthSummary { Status = "healthy" },
            NetworkSettings = new NetworkSettingsSummary { Networks = new Dictionary<string, EndpointSettings> { ["z"] = new(), ["a"] = new() } }
        };

        var info = DockerDiscoveryClient.Map(summary)!;

        Assert.Equal("example-web", info.ContainerName);
        Assert.True(info.IsCompose);
        Assert.True(info.IsRunning);
        Assert.Equal(new ushort[] { 80, 443 }, info.Ports);
        Assert.Equal((ushort)8080, info.PublishedPorts[80]);
        Assert.Equal("healthy", info.Health);
        Assert.Equal(new[] { "a", "z" }, info.Networks);
        Assert.Equal("api.custom.local", info.CustomDomains[0].Domain);
        Assert.Equal("localhost:8080", DevLocal.Core.DomainGenerator.DomainTargets("dev.local", [info])["api.custom.local"][0]);
    }

    [Theory]
    [InlineData("false")]
    [InlineData("NO")]
    [InlineData("0")]
    public void SkipsDisabledContainers(string label)
    {
        var summary = new ContainerListResponse
        {
            Labels = new Dictionary<string, string> { ["dev.local"] = label }
        };

        Assert.Null(DockerDiscoveryClient.Map(summary));
    }
}
