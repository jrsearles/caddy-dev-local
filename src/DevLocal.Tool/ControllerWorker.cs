using Microsoft.Extensions.Hosting;
using Microsoft.Extensions.Logging;

namespace DevLocal.Tool;

internal sealed class ControllerWorker(ServiceSettings settings, ILogger<ControllerWorker> logger) : BackgroundService
{
    protected override async Task ExecuteAsync(CancellationToken stoppingToken)
    {
        try
        {
            var options = settings.Options ?? throw new InvalidOperationException("Controller service options are missing");
            using var session = ControllerFactory.Create(options, (name, error) =>
                logger.LogError(error, "Hook {HookName} failed", name));
            await session.Controller.RunAsync(stoppingToken);
        }
        catch (OperationCanceledException) when (stoppingToken.IsCancellationRequested)
        {
        }
        catch (Exception error)
        {
            logger.LogCritical(error, "Controller stopped unexpectedly");
            Environment.Exit(1);
        }
    }
}
