using DevLocal.Core;

namespace DevLocal.Tool;

internal sealed class HostsHook(HostsFileReconciler file, string tld) : IUpdateHook
{
    public string Name => "hosts";

    public async Task ApplyAsync(DiscoveryUpdate update, CancellationToken token)
    {
        if (update.Status.LastRefresh is null && update.Status.LastError != "")
            return;

        await file.SyncAsync(tld, DomainGenerator.Domains(tld, update.Snapshot), token);
    }

    public Task CleanupAsync(CancellationToken token) => file.RemoveAsync(token);
}
