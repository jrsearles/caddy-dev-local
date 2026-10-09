using System.Net;
using System.Text.Json;
using DevLocal.Core;
using DevLocal.Ui;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class UiServerHostTests
{
    [Fact]
    public async Task ServesPageStylesAndBlazorScriptOnRandomLoopbackPort()
    {
        await using var host = new UiServerHost("dev.local");
        var port = await host.StartAsync(0, CancellationToken.None);

        Assert.InRange(port, 1024, 65535);
        using var client = new HttpClient();

        var page = await client.GetAsync($"http://127.0.0.1:{port}/");
        Assert.Equal(HttpStatusCode.OK, page.StatusCode);
        var html = await page.Content.ReadAsStringAsync();
        Assert.Contains("blazor.web.js", html);
        Assert.Contains("devlocal", html);

        var css = await client.GetAsync($"http://127.0.0.1:{port}/index.css");
        Assert.Equal(HttpStatusCode.OK, css.StatusCode);
        Assert.Equal("text/css", css.Content.Headers.ContentType?.MediaType);
        Assert.Contains(".card", await css.Content.ReadAsStringAsync());

        var script = await client.GetAsync($"http://127.0.0.1:{port}/_framework/blazor.web.js");
        Assert.Equal(HttpStatusCode.OK, script.StatusCode);
        Assert.Contains("blazor", await script.Content.ReadAsStringAsync());
    }

    [Fact]
    public async Task ServesQuickGridStylesAndScript()
    {
        await using var host = new UiServerHost("dev.local");
        var port = await host.StartAsync(0, CancellationToken.None);
        using var client = new HttpClient();

        var css = await client.GetAsync($"http://127.0.0.1:{port}/quickgrid.css");
        Assert.Equal(HttpStatusCode.OK, css.StatusCode);
        Assert.Equal("text/css", css.Content.Headers.ContentType?.MediaType);
        Assert.Contains("col-title", await css.Content.ReadAsStringAsync());

        var script = await client.GetAsync(
            $"http://127.0.0.1:{port}/_content/Microsoft.AspNetCore.Components.QuickGrid/QuickGrid.razor.js");
        Assert.Equal(HttpStatusCode.OK, script.StatusCode);
        Assert.Contains("init", await script.Content.ReadAsStringAsync());
    }

    [Fact]
    public async Task ServesInteractiveNegotiationEndpoint()
    {
        await using var host = new UiServerHost("dev.local");
        var port = await host.StartAsync(0, CancellationToken.None);
        using var client = new HttpClient();

        var negotiation = await client.PostAsync($"http://127.0.0.1:{port}/_blazor/negotiate?negotiateVersion=1", null);
        Assert.Equal(HttpStatusCode.OK, negotiation.StatusCode);
        using var document = JsonDocument.Parse(await negotiation.Content.ReadAsStringAsync());
        var ids = document.RootElement.GetProperty("connectionToken");
        Assert.False(ids.ValueKind == JsonValueKind.Null || ids.GetString() is null or "");
    }

    [Fact]
    public async Task PublishesDiscoveryStateIntoRenderedOutput()
    {
        await using var host = new UiServerHost("dev.local");
        var port = await host.StartAsync(0, CancellationToken.None);
        using var client = new HttpClient();

        host.State.Publish(new DiscoveryUpdate(
            [new ContainerInfo
            {
                ContainerId = "abcdef012345", ContainerName = "spike-web", Image = "nginx:alpine",
                IsRunning = true, Ports = [80], SelectedPort = 80
            }], new DiscoveryStatus(DateTimeOffset.FromUnixTimeSeconds(1700000000), "")), "");

        var html = await client.GetStringAsync($"http://127.0.0.1:{port}/");
        Assert.Contains("spike-web", html);
        Assert.Contains("spike-web.dev.local", html);
        Assert.Contains("spike-web.localhost", html);
    }

    [Fact]
    public async Task RendersReconnectBadgeInsteadOfBlockingOverlay()
    {
        await using var host = new UiServerHost("dev.local");
        var port = await host.StartAsync(0, CancellationToken.None);
        using var client = new HttpClient();

        var html = await client.GetStringAsync($"http://127.0.0.1:{port}/");
        Assert.Contains("id=\"components-reconnect-modal\"", html);
        Assert.Contains("reconnect-badge", html);

        var css = await client.GetStringAsync($"http://127.0.0.1:{port}/index.css");
        Assert.Contains("components-reconnect-show", css);
        Assert.DoesNotContain("components-reconnect-overlay", css);
    }

    [Fact]
    public async Task ReportsDiscoveryErrors()
    {
        await using var host = new UiServerHost("dev.local");
        var port = await host.StartAsync(0, CancellationToken.None);
        using var client = new HttpClient();

        host.State.Publish(new DiscoveryUpdate([], new DiscoveryStatus(null, "docker is down")), "");

        var html = await client.GetStringAsync($"http://127.0.0.1:{port}/");
        Assert.Contains("docker is down", html);
    }
}