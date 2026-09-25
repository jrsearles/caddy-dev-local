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
        builder.Services.AddSingleton<IDependencyProbe, DependencyMonitor>();
        builder.Services.AddHostedService<MonitorWorker>();
        await builder.Build().RunAsync();
        return 0;
    }

    Console.WriteLine("DevLocal Windows service prototype: devlocal service install|upgrade|status|logs|start|stop|restart|uninstall");
    return 0;
}
catch (Exception ex)
{
    Console.Error.WriteLine($"devlocal: {ex.Message}");
    return 1;
}
