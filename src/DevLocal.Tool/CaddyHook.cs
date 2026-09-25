using DevLocal.Core;

namespace DevLocal.Tool;

internal sealed class CaddyHook(ICaddyConfigClient client, string tld, bool tracing) : IUpdateHook
{
    public string Name => "caddy";

    public Task ApplyAsync(DiscoveryUpdate update, CancellationToken token)
    {
        if (update.Status.LastRefresh is null && update.Status.LastError != "")
            return Task.CompletedTask;

        var targets = DomainGenerator.DomainTargets(tld, update.Snapshot);
        return client.ReconcileAsync(CaddyResourceGenerator.Build(targets, tracing), CaddyResourceGenerator.Owns, token);
    }

    public Task CleanupAsync(CancellationToken token) => client.CleanupAsync(token);
}
