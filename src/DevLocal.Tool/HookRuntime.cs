using System.Threading.Channels;
using DevLocal.Core;

namespace DevLocal.Tool;

internal interface IUpdateHook
{
    string Name { get; }
    Task ApplyAsync(DiscoveryUpdate update, CancellationToken token);
    Task CleanupAsync(CancellationToken token);
}

internal sealed class HookSequence(string name, params IUpdateHook[] hooks) : IUpdateHook
{
    public string Name => name;

    public async Task ApplyAsync(DiscoveryUpdate update, CancellationToken token)
    {
        foreach (var hook in hooks)
            await hook.ApplyAsync(update, token);
    }

    public async Task CleanupAsync(CancellationToken token)
    {
        var errors = new List<Exception>();
        foreach (var hook in hooks)
        {
            try
            {
                await hook.CleanupAsync(token);
            }
            catch (Exception ex) when (!token.IsCancellationRequested)
            {
                errors.Add(new InvalidOperationException($"{hook.Name}: {ex.Message}", ex));
            }
        }
        if (errors.Count > 0)
            throw new AggregateException(errors);
    }
}

internal sealed class HookRuntime(Action<string, Exception>? onError = null)
{
    private readonly object gate = new();
    private readonly List<IUpdateHook> hooks = [];
    private List<Channel<DiscoveryUpdate>>? channels;
    private Task? workers;
    private CancellationToken token;

    internal void Register(IUpdateHook hook)
    {
        lock (gate)
        {
            if (channels is not null)
                throw new InvalidOperationException("Hook runtime already started");
            hooks.Add(hook);
        }
    }

    internal async Task ApplyOnceAsync(DiscoveryUpdate update, CancellationToken cancellationToken)
    {
        IUpdateHook[] snapshot;
        lock (gate)
        {
            if (channels is not null)
                throw new InvalidOperationException("Hook runtime already started");
            snapshot = hooks.ToArray();
        }
        await ApplyAllAsync(snapshot, update, cancellationToken);
    }

    internal async Task StartAsync(DiscoveryUpdate initial, CancellationToken cancellationToken)
    {
        IUpdateHook[] snapshot;
        lock (gate)
        {
            if (channels is not null)
                throw new InvalidOperationException("Hook runtime already started");
            snapshot = hooks.ToArray();
            channels = [];
        }

        await ApplyAllAsync(snapshot, initial, cancellationToken);
        lock (gate)
        {
            token = cancellationToken;
            foreach (var hook in snapshot)
                channels!.Add(Channel.CreateBounded<DiscoveryUpdate>(new BoundedChannelOptions(1)
                {
                    FullMode = BoundedChannelFullMode.DropOldest,
                    SingleReader = true,
                    SingleWriter = false
                }));
            workers = Task.WhenAll(snapshot.Select((hook, index) => WorkAsync(hook, channels[index].Reader, cancellationToken)));
        }
    }

    internal void Submit(DiscoveryUpdate update)
    {
        lock (gate)
        {
            if (channels is null || token.IsCancellationRequested)
                return;
            foreach (var channel in channels)
                channel.Writer.TryWrite(update);
        }
    }

    internal async Task WaitAsync()
    {
        Task? running;
        lock (gate)
            running = workers;
        if (running is null)
            throw new InvalidOperationException("Hook runtime not started");
        await running;
    }

    internal async Task CleanupAsync(CancellationToken cancellationToken)
    {
        IUpdateHook[] snapshot;
        lock (gate)
            snapshot = hooks.ToArray();
        var errors = new List<Exception>();
        foreach (var hook in snapshot)
        {
            try
            {
                await hook.CleanupAsync(cancellationToken);
            }
            catch (Exception ex) when (!cancellationToken.IsCancellationRequested)
            {
                errors.Add(new InvalidOperationException($"{hook.Name}: {ex.Message}", ex));
            }
        }
        if (errors.Count > 0)
            throw new AggregateException(errors);
    }

    private static async Task ApplyAllAsync(IUpdateHook[] hooks, DiscoveryUpdate update, CancellationToken token)
    {
        var errors = new List<Exception>();
        foreach (var hook in hooks)
        {
            try
            {
                await hook.ApplyAsync(update, token);
            }
            catch (Exception ex) when (!token.IsCancellationRequested)
            {
                errors.Add(new InvalidOperationException($"{hook.Name}: {ex.Message}", ex));
            }
        }
        if (errors.Count > 0)
            throw new AggregateException(errors);
    }

    private async Task WorkAsync(IUpdateHook hook, ChannelReader<DiscoveryUpdate> input, CancellationToken token)
    {
        try
        {
            await foreach (var update in input.ReadAllAsync(token))
            {
                try
                {
                    await hook.ApplyAsync(update, token);
                }
                catch (Exception ex) when (!token.IsCancellationRequested)
                {
                    onError?.Invoke(hook.Name, ex);
                }
            }
        }
        catch (OperationCanceledException) when (token.IsCancellationRequested)
        {
        }
    }
}
