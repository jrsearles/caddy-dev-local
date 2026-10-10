using DevLocal.Tool;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Hosting;
using Spectre.Console;

try
{
    if (args is ["service"])
    {
        Cli.Out.MarkupLine(
            "[bold]Usage:[/] devlocal service <command>\n" +
            "Commands:\n" +
            "  [aqua]install[/]    Install the background service (Administrator required)\n" +
            "  [aqua]upgrade[/]    Update the service binaries and restart it\n" +
            "  [aqua]uninstall[/]  Stop the service and remove it\n" +
            "  [aqua]start[/]      Start the installed service\n" +
            "  [aqua]stop[/]       Stop the running service\n" +
            "  [aqua]restart[/]    Restart the service\n" +
            "  [aqua]status[/]     Show whether the service is installed and running\n" +
            "  [aqua]logs[/]       Show the last 50 controller log lines");
        return 0;
    }
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
        Cli.Out.MarkupLine($"devlocal [aqua]{Markup.Escape(ToolVersion.Display)}[/]");
        return 0;
    }

    if (args.Length == 0 || args.Contains("--help", StringComparer.Ordinal))
    {
        if (args.Length == 0 && OperatingSystem.IsWindows())
            await ServiceManager.PrintStatusAsync(CancellationToken.None);
        if (args.Length == 0)
            Console.WriteLine();
        Cli.Out.MarkupLine(
            "[bold]Usage:[/] devlocal <command> [dim](flags)[/]\n" +
            "Commands:\n" +
            "  [aqua]start[/]    Reconcile routes, watch Docker events and poll; serves the index UI\n" +
            "  [aqua]clean[/]    Remove managed Caddy routes, TLS policies and the hosts block\n" +
            "  [aqua]service[/]  Manage the background service: install, upgrade, uninstall,\n" +
            "           start, stop, restart, status, logs\n" +
            "  [aqua]version[/]  Print the tool version and exit\n" +
            "Flags:\n" +
            "  [aqua]--tld[/] <domain>              Top-level domain for generated names [dim](dev.local)[/]\n" +
            "  [aqua]--ui-port[/] <port>            Fixed port for the index UI [dim](auto-picked)[/]\n" +
            "  [aqua]--caddy-admin[/] <url>         Caddy admin API endpoint [dim](http://localhost:2019)[/]\n" +
            "  [aqua]--caddy-server[/] <name>       Caddy server to configure [dim](srv0)[/]\n" +
            "  [aqua]--stale-ttl[/] <duration>      Age before unused domains are dropped [dim](1h)[/]\n" +
            "  [aqua]--poll-interval[/] <duration>  Docker poll interval [dim](30s)[/]\n" +
            "  [aqua]--probe-timeout[/] <duration>  HTTP port probe timeout [dim](2s)[/]\n" +
            "  [aqua]--allow-create-server[/]       Create the Caddy server if it is missing\n" +
            "  [aqua]--no-tracing[/]                Disable tracing output\n" +
            "  [aqua]--log-level[/] <level>         debug, info, warn or error [dim](info)[/]\n" +
            "Options come from flags, then DEVLOCAL_* env vars, then defaults;\n" +
            "dim values above are the defaults. The Docker named pipe is detected\n" +
            "from [aqua]docker context inspect[/].");
        return 0;
    }

    var settings = CommandLine.Parse(args);
    await using var session = await ControllerFactory.Create(settings.Options,
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
    Cli.Err.MarkupLine($"[red]devlocal:[/] {Markup.Escape(ex.Message)}");
    if (ex is ArgumentException)
        Cli.Err.MarkupLine("Run [aqua]devlocal --help[/] for usage.");
    return 1;
}
