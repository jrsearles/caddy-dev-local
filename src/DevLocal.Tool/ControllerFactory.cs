using DevLocal.Core;
using Docker.DotNet;

namespace DevLocal.Tool;

internal sealed class ControllerSession(Controller controller, IDisposable docker, CaddyApiClient? caddy) : IDisposable
{
    internal Controller Controller => controller;

    public void Dispose()
    {
        caddy?.Dispose();
        docker.Dispose();
    }
}

internal static class ControllerFactory
{
    internal static ControllerSession Create(AppOptions options, Action<string, Exception>? onError = null)
    {
        if (!OperatingSystem.IsWindows())
            throw new PlatformNotSupportedException("Live controller currently supports Windows only");

        var docker = new DockerClientBuilder()
            .WithEndpoint(new Uri($"npipe://./pipe/{options.DockerPipe}"))
            .Build();
        CaddyApiClient? caddy = null;
        try
        {
            if (options.Caddy || options.UI)
                caddy = new CaddyApiClient(options.CaddyAdmin, options.CaddyServer, options.AllowCreateServer);
            var source = new DockerContainerSource(docker);
            var discovery = new DiscoveryState(source, options.StaleTtl, options.ProbeTimeout,
                options.Caddy || options.UI ? new HttpPortProbe() : null);
            var runtime = new HookRuntime(onError ?? ((name, error) => Console.Error.WriteLine($"{name}: {error}")));
            if (options.Caddy && options.UI)
                runtime.Register(new HookSequence("caddy-ui",
                    new CaddyHook(caddy!, options.Tld, options.Tracing),
                    new UiHook(options.Tld, options.IndexDir, caddy)));
            else if (options.Caddy)
                runtime.Register(new CaddyHook(caddy!, options.Tld, options.Tracing));
            else if (options.UI)
                runtime.Register(new UiHook(options.Tld, options.IndexDir, caddy));
            if (options.HostsFile)
            {
                var path = HostsFileReconciler.WindowsHostsPath;
                var writable = CanWrite(path);
                if (!writable)
                    Console.Error.WriteLine($"Hosts file is not writable: {path}");
                runtime.Register(new HostsHook(new HostsFileReconciler(path), options.Tld, true, writable));
            }

            return new ControllerSession(new Controller(discovery, runtime,
                new DiscoveryRunner(discovery, new DockerEventSource(docker), options.PollInterval)), docker, caddy);
        }
        catch
        {
            caddy?.Dispose();
            docker.Dispose();
            throw;
        }
    }

    private static bool CanWrite(string path)
    {
        try
        {
            using var file = File.Open(path, FileMode.Open, FileAccess.Write, FileShare.ReadWrite);
            return true;
        }
        catch (IOException) { return false; }
        catch (UnauthorizedAccessException) { return false; }
    }
}
