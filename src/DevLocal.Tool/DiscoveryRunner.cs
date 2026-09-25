using System.Threading.Channels;
using DevLocal.Core;
using Docker.DotNet;

namespace DevLocal.Tool;

internal interface IDockerEventSource
{
    Task WatchAsync(Action onRefresh, CancellationToken token);
}

internal sealed class DockerEventSource(IDockerClient client) : IDockerEventSource
{
    public Task WatchAsync(Action onRefresh, CancellationToken token) =>
        DockerEventWatcher.WatchAsync(client, onRefresh, token);
}

internal sealed class DiscoveryRunner(
    DiscoveryState state,
    IDockerEventSource events,
    TimeSpan pollInterval)
{
    private readonly Channel<DiscoveryUpdate> updates = Channel.CreateBounded<DiscoveryUpdate>(new BoundedChannelOptions(1)
    {
        FullMode = BoundedChannelFullMode.DropOldest,
        SingleReader = true,
        SingleWriter = false
    });
    private readonly Channel<bool> triggers = Channel.CreateBounded<bool>(new BoundedChannelOptions(1)
    {
        FullMode = BoundedChannelFullMode.DropOldest,
        SingleReader = true,
        SingleWriter = false
    });
    private int started;

    internal ChannelReader<DiscoveryUpdate> Updates => updates.Reader;

    internal async Task RunAsync(CancellationToken token)
    {
        if (Interlocked.Exchange(ref started, 1) != 0)
            throw new InvalidOperationException("Discovery already started");

        try
        {
            var watch = WatchEventsAsync(token);
            Publish((await state.RefreshAsync(token)).Update);
            var tasks = new List<Task>
            {
                watch,
                ConsumeTriggersAsync(token),
                PollAsync(TimeSpan.FromMinutes(5), token)
            };
            if (pollInterval > TimeSpan.Zero)
                tasks.Add(PollAsync(pollInterval, token));
            await Task.WhenAll(tasks);
        }
        catch (OperationCanceledException) when (token.IsCancellationRequested)
        {
        }
        finally
        {
            updates.Writer.TryComplete();
        }
    }

    private void Publish(DiscoveryUpdate update) => updates.Writer.TryWrite(update);

    private async Task RefreshAsync(CancellationToken token) => Publish((await state.RefreshAsync(token)).Update);

    private async Task WatchEventsAsync(CancellationToken token)
    {
        try
        {
            while (!token.IsCancellationRequested)
            {
                try
                {
                    await events.WatchAsync(() => triggers.Writer.TryWrite(true), token);
                    if (!token.IsCancellationRequested)
                        Publish(state.ReportError("Docker event stream closed, reconnecting"));
                }
                catch (Exception ex) when (!token.IsCancellationRequested)
                {
                    Publish(state.ReportError("Docker event stream error: " + ex.Message));
                }
                await Task.Delay(TimeSpan.FromSeconds(30), token);
            }
        }
        catch (OperationCanceledException) when (token.IsCancellationRequested)
        {
        }
    }

    private async Task ConsumeTriggersAsync(CancellationToken token)
    {
        try
        {
            while (await triggers.Reader.WaitToReadAsync(token))
            {
                while (triggers.Reader.TryRead(out _)) { }
                await Task.Delay(TimeSpan.FromMilliseconds(100), token);
                while (triggers.Reader.TryRead(out _)) { }
                await RefreshAsync(token);
            }
        }
        catch (OperationCanceledException) when (token.IsCancellationRequested)
        {
        }
    }

    private async Task PollAsync(TimeSpan interval, CancellationToken token)
    {
        using var timer = new PeriodicTimer(interval);
        try
        {
            while (await timer.WaitForNextTickAsync(token))
                await RefreshAsync(token);
        }
        catch (OperationCanceledException) when (token.IsCancellationRequested)
        {
        }
    }
}
