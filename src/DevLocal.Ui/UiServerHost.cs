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
            MapQuickGridAssets(web);
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

    private const string QuickGridPackage = "Microsoft.AspNetCore.Components.QuickGrid";

    private static void MapFrameworkScripts(WebApplication web)
    {
        var assets = StaticAssets("_framework").ToList();
        foreach (var name in new[] { "blazor.web.js", "blazor.server.js" })
        {
            var script = assets.FirstOrDefault(asset => asset.Name == name);
            if (script is { Path: { } path })
                MapFile(web, $"/_framework/{name}", path, "text/javascript");
        }
    }

    private static void MapQuickGridAssets(WebApplication web)
    {
        var assets = StaticAssets("_content", QuickGridPackage).ToList();
        var script = assets.FirstOrDefault(asset => asset.Name == "QuickGrid.razor.js");
        if (script is { Path: { } scriptPath })
            MapFile(web, $"/_content/{QuickGridPackage}/QuickGrid.razor.js", scriptPath, "text/javascript");
        var css = assets.FirstOrDefault(asset => asset.Name.EndsWith(".bundle.scp.css", StringComparison.Ordinal));
        if (css is { Path: { } cssPath })
            MapFile(web, "/quickgrid.css", cssPath, "text/css");
    }

    private static void MapFile(WebApplication web, string route, string path, string contentType)
    {
        var bytes = File.ReadAllBytes(path);
        web.MapGet(route, () => Results.Bytes(bytes, contentType));
    }

    private static IEnumerable<StaticAsset> StaticAssets(params string[] segments)
    {
        var directory = Path.Combine([AppContext.BaseDirectory, "wwwroot", .. segments]);
        if (Directory.Exists(directory))
            foreach (var file in Directory.EnumerateFiles(directory))
                yield return new StaticAsset(Path.GetFileName(file), file);

        foreach (var manifest in Directory.EnumerateFiles(AppContext.BaseDirectory, "*.staticwebassets.runtime.json"))
        {
            var assets = ManifestAssets(manifest, segments);
            if (assets is null)
                continue;
            foreach (var asset in assets)
                yield return asset;
        }
    }

    private static IReadOnlyList<StaticAsset>? ManifestAssets(string manifestPath, string[] segments)
    {
        try
        {
            using var document = JsonDocument.Parse(File.ReadAllText(manifestPath));
            var root = document.RootElement;
            if (!root.TryGetProperty("Root", out var tree)
                || !tree.TryGetProperty("Children", out var children)
                || !root.TryGetProperty("ContentRoots", out var contentRoots)
                || !TryDescend(children, segments, out var folder))
                return null;
            var assets = new List<StaticAsset>();
            foreach (var entry in folder.EnumerateObject())
            {
                if (!entry.Value.TryGetProperty("Asset", out var asset)
                    || asset.ValueKind != JsonValueKind.Object
                    || !asset.TryGetProperty("ContentRootIndex", out var indexElement)
                    || !indexElement.TryGetInt32(out var index)
                    || index >= contentRoots.GetArrayLength())
                    continue;
                var contentRoot = contentRoots[index].GetString();
                var subPath = asset.TryGetProperty("SubPath", out var sub) ? sub.GetString() : null;
                if (contentRoot is null || subPath is null)
                    continue;
                var candidate = Path.Combine(contentRoot, subPath);
                if (File.Exists(candidate))
                    assets.Add(new StaticAsset(entry.Name, candidate));
            }
            return assets;
        }
        catch (Exception) { return null; }
    }

    private static bool TryDescend(JsonElement children, string[] segments, out JsonElement folder)
    {
        folder = children;
        foreach (var segment in segments)
        {
            if (folder.ValueKind != JsonValueKind.Object
                || !folder.TryGetProperty(segment, out var child)
                || !child.TryGetProperty("Children", out folder))
                return false;
        }
        return true;
    }

    private sealed record StaticAsset(string Name, string Path);

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