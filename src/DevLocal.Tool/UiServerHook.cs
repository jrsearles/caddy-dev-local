using System.Collections.Immutable;
using System.Text.Json;
using DevLocal.Core;
using DevLocal.Ui;

namespace DevLocal.Tool;

internal sealed class UiServerHook(string tld, int requestedPort, ICaddyConfigClient? caddy, Action<string>? onLog = null)
    : IUpdateHook, IAsyncDisposable
{
    private readonly SemaphoreSlim gate = new(1, 1);
    private readonly UiServerHost host = new(tld);
    private string cachedConfig = "";
    private bool announced;

    public string Name => "ui";

    internal UiState State => host.State;

    internal string BaseUrl => host.BaseUrl;

    public async Task ApplyAsync(DiscoveryUpdate update, CancellationToken token)
    {
        await gate.WaitAsync(token);
        try
        {
            var port = await host.StartAsync(requestedPort, token);
            if (!announced)
            {
                announced = true;
                onLog?.Invoke($"Index UI served from {host.BaseUrl}");
            }
            if (caddy is not null)
            {
                try
                {
                    cachedConfig = await caddy.RunningConfigAsync(token);
                }
                catch (Exception) when (!token.IsCancellationRequested)
                {
                }
            }

            host.State.Publish(update, cachedConfig);
            if (caddy is not null)
                await caddy.ReconcileAsync(Resources(update.Snapshot, port), Owns, token);
        }
        finally
        {
            gate.Release();
        }
    }

    public async Task CleanupAsync(CancellationToken token)
    {
        await gate.WaitAsync(token);
        try
        {
            if (caddy is not null)
                await caddy.CleanupAsync(token);
        }
        finally
        {
            gate.Release();
        }
    }

    public async ValueTask DisposeAsync()
    {
        await host.DisposeAsync();
        gate.Dispose();
    }

    private static bool Owns(string id) => id is "devlocal-index" or "devlocal-tls-ui";

    private CaddyResources Resources(ImmutableArray<ContainerInfo> containers, int port)
    {
        var reserved = DomainGenerator.DomainTargets(tld, containers).Keys.ToHashSet(StringComparer.Ordinal);
        var uiHost = DomainGenerator.TldLocalhost(tld);
        var hosts = reserved.Contains(uiHost) ? [] : new[] { uiHost };
        var routes = ImmutableDictionary.CreateBuilder<string, JsonElement>(StringComparer.Ordinal);
        var policies = ImmutableDictionary.CreateBuilder<string, JsonElement>(StringComparer.Ordinal);
        if (hosts.Length != 0)
        {
            routes["devlocal-index"] = JsonSerializer.SerializeToElement(new Dictionary<string, object>
            {
                ["@id"] = "devlocal-index",
                ["handle"] = new object[] { new Dictionary<string, object>
                {
                    ["handler"] = "reverse_proxy",
                    ["upstreams"] = new object[] { new Dictionary<string, object>
                    {
                        ["dial"] = $"127.0.0.1:{port}"
                    } }
                } },
                ["match"] = new object[] { new Dictionary<string, object> { ["host"] = hosts } },
                ["terminal"] = true
            });
            policies["devlocal-tls-ui"] = JsonSerializer.SerializeToElement(new Dictionary<string, object>
            {
                ["@id"] = "devlocal-tls-ui",
                ["issuers"] = new object[] { new Dictionary<string, object> { ["module"] = "internal" } },
                ["subjects"] = hosts
            });
        }
        return new CaddyResources(routes.ToImmutable(), policies.ToImmutable());
    }
}