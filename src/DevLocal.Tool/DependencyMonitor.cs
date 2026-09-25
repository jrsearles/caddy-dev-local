using System.Net.Http;
using Docker.DotNet;

namespace DevLocal.Tool;

internal interface IDependencyProbe
{
    Task CheckDockerAsync(string pipe, CancellationToken token);
    Task CheckCaddyAsync(string adminUrl, CancellationToken token);
}

internal sealed class DependencyMonitor : IDependencyProbe, IDisposable
{
    private readonly HttpClient caddy = new() { Timeout = TimeSpan.FromSeconds(3) };

    public async Task CheckDockerAsync(string pipe, CancellationToken token)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(token);
        timeout.CancelAfter(TimeSpan.FromSeconds(3));
        using var client = new DockerClientBuilder()
            .WithEndpoint(new Uri($"npipe://./pipe/{pipe}"))
            .Build();
        await DockerDiscoveryClient.ListAsync(client, timeout.Token);
    }

    public async Task CheckCaddyAsync(string adminUrl, CancellationToken token)
    {
        using var response = await caddy.GetAsync(new Uri(new Uri(adminUrl.TrimEnd('/') + "/"), "config/"), token);
        response.EnsureSuccessStatusCode();
    }

    public void Dispose() => caddy.Dispose();
}

internal sealed record ProbeResult(bool Connected, string? Error)
{
    internal static ProbeResult Ready => new(true, null);
}

internal sealed record MonitorStatus(DateTimeOffset CheckedAt, ProbeResult Docker, ProbeResult Caddy);

internal static class MonitorCycle
{
    internal static async Task<MonitorStatus> CheckAsync(
        IDependencyProbe probe, ServiceSettings settings, CancellationToken token)
    {
        static async Task<ProbeResult> TryAsync(Func<Task> action, CancellationToken token)
        {
            try
            {
                await action();
                return ProbeResult.Ready;
            }
            catch (OperationCanceledException) when (token.IsCancellationRequested)
            {
                throw;
            }
            catch (Exception ex)
            {
                return new ProbeResult(false, ex.Message);
            }
        }

        var docker = await TryAsync(() => probe.CheckDockerAsync(settings.DockerPipe, token), token);
        var caddy = await TryAsync(() => probe.CheckCaddyAsync(settings.CaddyAdmin, token), token);
        return new MonitorStatus(DateTimeOffset.UtcNow, docker, caddy);
    }
}
