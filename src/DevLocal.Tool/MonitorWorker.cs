using System.Text.Json;
using Microsoft.Extensions.Hosting;

namespace DevLocal.Tool;

internal sealed class MonitorWorker(IDependencyProbe probe) : BackgroundService
{
    protected override async Task ExecuteAsync(CancellationToken stoppingToken)
    {
        var settings = await ServiceSettings.LoadAsync(stoppingToken);
        Directory.CreateDirectory(ServicePaths.DataDirectory);
        MonitorStatus? previous = null;
        while (!stoppingToken.IsCancellationRequested)
        {
            var status = await MonitorCycle.CheckAsync(probe, settings, stoppingToken);
            var text = JsonSerializer.Serialize(status);
            await File.WriteAllTextAsync(ServicePaths.StatusPath, text, stoppingToken);
            if (previous is null || previous.Docker != status.Docker || previous.Caddy != status.Caddy)
                await File.AppendAllTextAsync(ServicePaths.LogPath, text + Environment.NewLine, stoppingToken);
            previous = status;
            await Task.Delay(TimeSpan.FromSeconds(10), stoppingToken);
        }
    }
}
