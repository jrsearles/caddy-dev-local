using DevLocal.Tool;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Hosting;

try
{
    if (args.Length > 0 && args[0] == "service" && args.Length > 1 && args[1] != "run")
        return await ServiceManager.RunAsync(args[1..], CancellationToken.None);

    if (args is ["service", "run"])
    {
        var builder = Host.CreateApplicationBuilder();
        builder.Services.AddWindowsService(options => options.ServiceName = ServicePaths.Name);
        builder.Services.AddSingleton(await ServiceSettings.LoadAsync(CancellationToken.None));
        builder.Services.AddHostedService<ControllerWorker>();
        await builder.Build().RunAsync();
        return 0;
    }

    if (args is ["version"] || args.Contains("--version", StringComparer.Ordinal))
    {
        Console.WriteLine($"devlocal {ToolVersion.Display}");
        return 0;
    }

    if (args.Length == 0 || args.Contains("--help", StringComparer.Ordinal))
    {
        if (args.Length == 0 && OperatingSystem.IsWindows())
            await ServiceManager.PrintStatusAsync(CancellationToken.None);
        if (args.Length == 0)
            Console.WriteLine();
        Console.WriteLine("Usage: devlocal [start|clean|version] [flags]\n" +
            "       devlocal service install|upgrade|status|logs|start|stop|restart|uninstall\n" +
            "Commands:\n" +
            "  start    Reconcile, then watch Docker events and poll, serving the index UI\n" +
            "  clean    Remove managed Caddy routes, TLS policies and the hosts block\n" +
            "  version  Print the tool version and exit\n" +
            "Flags: --tld --stale-ttl --probe-timeout --poll-interval --hosts-file --no-tracing " +
            "--caddy --ui --ui-port --caddy-admin --caddy-server --allow-create-server --log-level --docker-pipe\n" +
            "Options come from flags, then DEVLOCAL_* env vars, then defaults.");
        return 0;
    }

    var settings = CommandLine.Parse(args);
    if (!args.Any(arg => arg == "--docker-pipe" || arg.StartsWith("--docker-pipe=", StringComparison.Ordinal)))
    {
        var pipe = await DockerContext.CurrentPipeAsync(CancellationToken.None);
        if (pipe is not null)
            settings = settings with { Options = settings.Options with { DockerPipe = pipe } };
    }
    await using var session = ControllerFactory.Create(settings.Options,
        onError: (message, ex) => Console.Error.WriteLine($"devlocal: {message}: {ex.Message}"),
        onLog: message => Console.WriteLine(message));
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
    }
    return 0;
}
catch (Exception ex)
{
    Console.Error.WriteLine($"devlocal: {ex.Message}");
    return 1;
}
