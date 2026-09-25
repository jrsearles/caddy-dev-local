using DevLocal.Tool;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Hosting;

try
{
    if (args.Length > 0 && args[0] == "service" && args.Length > 1 && args[1] != "run")
        return await ServiceManager.RunAsync(args[1..], CancellationToken.None);

    if (args is ["service", "run"])
    {
        var serviceSettings = await ServiceSettings.LoadAsync(CancellationToken.None);
        var builder = Host.CreateApplicationBuilder();
        builder.Services.AddWindowsService(options => options.ServiceName = ServicePaths.Name);
        if (serviceSettings.ControllerEnabled)
        {
            builder.Services.AddSingleton(serviceSettings);
            builder.Services.AddHostedService<ControllerWorker>();
        }
        else
        {
            builder.Services.AddSingleton<IDependencyProbe, DependencyMonitor>();
            builder.Services.AddHostedService<MonitorWorker>();
        }
        await builder.Build().RunAsync();
        return 0;
    }

    if (args.Contains("--help", StringComparer.Ordinal))
    {
        Console.WriteLine("Usage: devlocal [start|clean] [flags]\n" +
            "       devlocal service install|upgrade|status|logs|start|stop|restart|uninstall\n" +
            "Flags: --tld --stale-ttl --probe-timeout --poll-interval --hosts-file --no-tracing " +
            "--caddy --ui --caddy-admin --caddy-server --allow-create-server --index-dir --log-level --docker-pipe");
        return 0;
    }

    var settings = CommandLine.Parse(args);
    if (!args.Any(arg => arg == "--docker-pipe" || arg.StartsWith("--docker-pipe=", StringComparison.Ordinal)))
    {
        var pipe = await DockerContext.CurrentPipeAsync(CancellationToken.None);
        if (pipe is not null)
            settings = settings with { Options = settings.Options with { DockerPipe = pipe } };
    }
    using var session = ControllerFactory.Create(settings.Options);
    using var stop = new CancellationTokenSource();
    Console.CancelKeyPress += (_, e) => { e.Cancel = true; stop.Cancel(); };
    switch (settings.Mode)
    {
        case RunMode.Start:
            await session.Controller.RunAsync(stop.Token);
            break;
        case RunMode.Clean:
            await session.Controller.CleanupAsync(stop.Token);
            break;
        default:
            await session.Controller.RunOnceAsync(stop.Token);
            break;
    }
    return 0;
}
catch (Exception ex)
{
    Console.Error.WriteLine($"devlocal: {ex.Message}");
    return 1;
}
