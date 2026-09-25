using DevLocal.Core;

namespace DevLocal.Tool;

internal sealed class Controller(DiscoveryState discovery, HookRuntime hooks, DiscoveryRunner? runner = null)
{
    internal async Task RunOnceAsync(CancellationToken token)
    {
        var result = await discovery.RefreshAsync(token);
        if (result.Error is not null)
            throw new InvalidOperationException("Listing Docker containers failed", result.Error);
        await hooks.ApplyOnceAsync(result.Update, token);
    }

    internal async Task RunAsync(CancellationToken token)
    {
        if (runner is null)
            throw new InvalidOperationException("Continuous discovery is not configured");

        using var lifetime = CancellationTokenSource.CreateLinkedTokenSource(token);
        var running = runner.RunAsync(lifetime.Token);
        var started = false;
        try
        {
            var initial = await runner.Updates.ReadAsync(lifetime.Token);
            await hooks.StartAsync(initial, lifetime.Token);
            started = true;
            await foreach (var update in runner.Updates.ReadAllAsync(lifetime.Token))
                hooks.Submit(update);
        }
        catch (OperationCanceledException) when (token.IsCancellationRequested)
        {
        }
        finally
        {
            lifetime.Cancel();
            await running;
            if (started)
                await hooks.WaitAsync();
        }
    }

    internal Task CleanupAsync(CancellationToken token) => hooks.CleanupAsync(token);
}
