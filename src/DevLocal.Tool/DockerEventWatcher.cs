using Docker.DotNet;
using Docker.DotNet.Models;

namespace DevLocal.Tool;

internal static class DockerEventWatcher
{
    internal static ContainerEventsParameters Parameters() => new()
    {
        Filters = new Dictionary<string, IDictionary<string, bool>>
        {
            ["type"] = new Dictionary<string, bool> { ["container"] = true, ["network"] = true }
        }
    };

    internal static bool ShouldRefresh(Message message) => message.Type switch
    {
        "container" => message.Action is "start" or "stop" or "die" or "destroy" or "create",
        "network" => message.Action is "connect" or "disconnect",
        _ => false
    };

    internal static Task WatchAsync(IDockerClient client, Action refresh, CancellationToken token) =>
        client.System.MonitorEventsAsync(Parameters(), new InlineProgress(message =>
        {
            if (ShouldRefresh(message))
                refresh();
        }), token);

    private sealed class InlineProgress(Action<Message> onMessage) : IProgress<Message>
    {
        public void Report(Message value) => onMessage(value);
    }
}
