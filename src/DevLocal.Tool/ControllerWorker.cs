using Microsoft.Extensions.Hosting;
using Microsoft.Extensions.Logging;

namespace DevLocal.Tool;

internal sealed class ControllerWorker(ServiceSettings settings, ILogger<ControllerWorker> logger) : BackgroundService
{
    protected override async Task ExecuteAsync(CancellationToken stoppingToken)
    {
        var reporter = new ControllerStatusReporter(ServicePaths.ControllerStatusPath, ServicePaths.ControllerLogPath);
        try
        {
            var options = settings.Options ?? throw new InvalidOperationException("Controller service options are missing");
            await using var session = await ControllerFactory.Create(options, (name, error) =>
            {
                reporter.Record(name, error);
                logger.LogError(error, "Hook {HookName} failed", name);
            },
            message =>
            {
                reporter.Announce(message);
                logger.LogInformation("{Message}", message);
            });
            using var heartbeat = CancellationTokenSource.CreateLinkedTokenSource(stoppingToken);
            var beating = HeartbeatAsync(session.Controller, reporter, heartbeat.Token);
            try
            {
                await session.Controller.RunAsync(stoppingToken);
            }
            finally
            {
                await heartbeat.CancelAsync();
                await beating;
                await reporter.WriteAsync(session.Controller.HookNames, CancellationToken.None);
            }
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

    private static async Task HeartbeatAsync(Controller controller, ControllerStatusReporter reporter, CancellationToken token)
    {
        try
        {
            while (!token.IsCancellationRequested)
            {
                await reporter.WriteAsync(controller.HookNames, token);
                await Task.Delay(TimeSpan.FromSeconds(10), token);
            }
        }
        catch (OperationCanceledException)
        {
        }
        catch (IOException error)
        {
            reporter.Record("status", error);
        }
        catch (UnauthorizedAccessException error)
        {
            reporter.Record("status", error);
        }
    }
}