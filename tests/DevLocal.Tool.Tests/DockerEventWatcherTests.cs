using DevLocal.Tool;
using Docker.DotNet.Models;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class DockerEventWatcherTests
{
    [Theory]
    [InlineData("container", "start", true)]
    [InlineData("container", "die", true)]
    [InlineData("container", "destroy", true)]
    [InlineData("container", "update", false)]
    [InlineData("network", "connect", true)]
    [InlineData("network", "disconnect", true)]
    [InlineData("network", "create", false)]
    [InlineData("image", "create", false)]
    public void RefreshesOnlyOnGoDiscoveryTriggers(string type, string action, bool expected) =>
        Assert.Equal(expected, DockerEventWatcher.ShouldRefresh(new Message { Type = type, Action = action }));

    [Fact]
    public void SubscribesToContainerAndNetworkEvents()
    {
        var filters = DockerEventWatcher.Parameters().Filters!;
        Assert.True(filters["type"]["container"]);
        Assert.True(filters["type"]["network"]);
        Assert.Single(filters);
    }
}
