using DevLocal.Core;
using DevLocal.Ui;
using Docker.DotNet;

namespace DevLocal.Tool;

internal sealed class ControllerSession(Controller controller, IDisposable docker, CaddyApiClient? caddy) : IAsyncDisposable
{
    internal Controller Controller => controller;

    public void Dispose()
    {
        caddy?.Dispose();
        docker.Dispose();
    }

    public async ValueTask DisposeAsync()
    {
        await controller.DisposeAsync();
        Dispose();
    }
}

internal static class ControllerFactory
{
    internal static async Task<ControllerSession> Create(AppOptions options,
        Action<string, Exception>? onError = null, Action<string>? onLog = null)
    {
        if (!OperatingSystem.IsWindows())
            throw new PlatformNotSupportedException("Live controller currently supports Windows only");

        var docker = new DockerClientBuilder()
            .WithEndpoint(new Uri($"npipe://./pipe/{await CurrentPipeAsync()}"))
            .Build();
        CaddyApiClient? caddy = null;
        try
        {
            caddy = new CaddyApiClient(options.CaddyAdmin, options.CaddyServer, options.AllowCreateServer);
            if (!await ReachableAsync(caddy))
                Console.Error.WriteLine($"Warning: Caddy admin API at {options.CaddyAdmin} is not reachable, updates will keep failing");
            var source = new DockerContainerSource(docker);
            var discovery = new DiscoveryState(source, options.StaleTtl, options.ProbeTimeout, new HttpPortProbe());
            var runtime = new HookRuntime(onError ?? ((name, error) => Console.Error.WriteLine($"{name}: {error}")));
            runtime.Register(new HookSequence("caddy-ui",
                new CaddyHook(caddy, options.Tld, options.Tracing),
                new UiServerHook(options.Tld, options.UiPort, caddy, onLog)));
            var hostsPath = HostsFileReconciler.WindowsHostsPath;
            if (!CanWrite(hostsPath))
                Console.Error.WriteLine($"Warning: hosts file is not writable, updates will keep failing: {hostsPath}");
            runtime.Register(new HostsHook(new HostsFileReconciler(hostsPath), options.Tld));

            return new ControllerSession(new Controller(runtime,
                new DiscoveryRunner(discovery, new DockerEventSource(docker), options.PollInterval)), docker, caddy);
        }
        catch
        {
            caddy?.Dispose();
            docker.Dispose();
            throw;
        }
    }

    private static async Task<string> CurrentPipeAsync()
    {
        var pipe = await DockerContext.CurrentPipeAsync(CancellationToken.None);
        return pipe ?? "docker_engine";
    }

    private static async Task<bool> ReachableAsync(CaddyApiClient caddy)
    {
        try
        {
            using var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(2));
            await caddy.RunningConfigAsync(timeout.Token);
            return true;
        }
        catch (Exception)
        {
            return false;
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
