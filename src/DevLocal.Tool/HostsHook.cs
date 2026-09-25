using DevLocal.Core;

namespace DevLocal.Tool;

internal sealed class HostsHook(HostsFileReconciler file, string tld, bool enabled, bool writable)
{
    internal async Task ApplyAsync(DiscoveryUpdate update, CancellationToken token)
    {
        if (!enabled || !writable || (update.Status.LastRefresh is null && update.Status.LastError != ""))
            return;

        await file.SyncAsync(tld, DomainGenerator.Domains(tld, update.Snapshot), token);
    }

    internal Task CleanupAsync(CancellationToken token) =>
        enabled ? file.RemoveAsync(token) : Task.CompletedTask;
}
