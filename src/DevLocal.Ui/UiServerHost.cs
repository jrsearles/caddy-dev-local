using System.Net;
using System.Text.Json;
using DevLocal.Ui.Components;
using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Hosting.Server;
using Microsoft.AspNetCore.Hosting.Server.Features;
using Microsoft.AspNetCore.Http;
using Microsoft.AspNetCore.Routing;
using Microsoft.AspNetCore.Server.Kestrel.Core;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.FileProviders;
using Microsoft.Extensions.Hosting;
using Microsoft.Extensions.Logging;

namespace DevLocal.Ui;

public sealed class UiServerHost(string tld) : IAsyncDisposable
{
    private readonly SemaphoreSlim gate = new(1, 1);
    private readonly UiState state = new();
    private WebApplication? app;
    private int port;

    public UiState State => state;

    public int Port
    {
        get
        {
            gate.Wait();
            try { return port; }
            finally { gate.Release(); }
        }
    }

    public string BaseUrl => $"http://127.0.0.1:{Port}";

    public async Task<int> StartAsync(int requestedPort, CancellationToken token)
    {
        await gate.WaitAsync(token);
        try
        {
            if (app is not null)
                return port;

            var builder = WebApplication.CreateBuilder(new WebApplicationOptions
            {
                EnvironmentName = Environments.Production,
                ApplicationName = typeof(UiServerHost).Assembly.GetName().Name,
                ContentRootPath = AppContext.BaseDirectory,
                WebRootPath = "wwwroot"
            });
            builder.Logging.ClearProviders();
            builder.WebHost.ConfigureKestrel(options =>
            {
                options.Listen(IPAddress.Loopback, requestedPort, listen => listen.Protocols = HttpProtocols.Http1);
                options.AddServerHeader = false;
            });
            builder.Services.AddRazorComponents().AddInteractiveServerComponents();
            builder.Services.AddSingleton(state);
            builder.Services.AddSingleton(new TldProvider(tld));
            builder.Services.AddScoped<UiSession>();
            builder.Services.AddScoped<UiInterop>();
            builder.Services.AddSingleton<Clock>();

            var web = builder.Build();
            web.Use(async (context, next) =>
            {
                context.Response.Headers.CacheControl = "no-store";
                await next();
            });
            web.UseStaticFiles();
            web.UseRouting();
            web.UseAntiforgery();
            MapFrameworkScripts(web);
            web.MapGet("/index.css", () => Results.Text(UiAssets.Css, "text/css"));
            web.MapGet("/ui.js", () => Results.Text(UiAssets.Js, "text/javascript"));
            web.MapRazorComponents<App>().AddInteractiveServerRenderMode();
            await web.StartAsync(token);
            app = web;
            port = ResolvePort(web);
            return port;
        }
        finally
        {
            gate.Release();
        }
    }

    private static void MapFrameworkScripts(WebApplication web)
    {
        foreach (var name in new[] { "blazor.web.js", "blazor.server.js" })
        {
            var path = ResolveFrameworkAsset(name);
            if (path is null)
                continue;
            var bytes = File.ReadAllBytes(path);
            web.MapGet($"/_framework/{name}", () => Results.Bytes(bytes, "text/javascript"));
        }
    }

    private static string? ResolveFrameworkAsset(string name)
    {
        var physical = Path.Combine(AppContext.BaseDirectory, "wwwroot", "_framework", name);
        if (File.Exists(physical))
            return physical;
        foreach (var manifest in Directory.EnumerateFiles(AppContext.BaseDirectory, "*.staticwebassets.runtime.json"))
        {
            string? resolved = null;
            try
            {
                using var document = JsonDocument.Parse(File.ReadAllText(manifest));
                var root = document.RootElement;
                if (!TryAsset(root, name, out var asset))
                    continue;
                var contentRoots = root.GetProperty("ContentRoots");
                var index = asset.GetProperty("ContentRootIndex").GetInt32();
                if (index >= contentRoots.GetArrayLength())
                    continue;
                var contentRoot = contentRoots[index].GetString();
                if (contentRoot is null)
                    continue;
                var candidate = Path.Combine(contentRoot, asset.GetProperty("SubPath").GetString() ?? "");
                if (File.Exists(candidate))
                    resolved = candidate;
            }
            catch (Exception) { }
            if (resolved is not null)
                return resolved;
        }
        return null;
    }

    private static bool TryAsset(JsonElement manifest, string name, out JsonElement asset)
    {
        asset = default;
        return manifest.TryGetProperty("Root", out var root)
            && root.TryGetProperty("Children", out var children)
            && children.TryGetProperty("_framework", out var framework)
            && framework.TryGetProperty("Children", out var scripts)
            && scripts.TryGetProperty(name, out var entry)
            && entry.TryGetProperty("Asset", out asset)
            && asset.ValueKind == JsonValueKind.Object;
    }

    private static int ResolvePort(WebApplication web)
    {
        var addresses = web.Services.GetRequiredService<IServer>()
            .Features.Get<IServerAddressesFeature>();
        var url = addresses?.Addresses.FirstOrDefault();
        return url is not null && Uri.TryCreate(url, UriKind.Absolute, out var uri) ? uri.Port : 0;
    }

    public async ValueTask DisposeAsync()
    {
        WebApplication? running;
        await gate.WaitAsync();
        try { running = app; app = null; }
        finally { gate.Release(); }
        if (running is null)
            return;
        try { await running.StopAsync(TimeSpan.FromSeconds(5)); }
        catch (Exception) { }
        await running.DisposeAsync();
        gate.Dispose();
    }
}

public sealed record TldProvider(string Value);