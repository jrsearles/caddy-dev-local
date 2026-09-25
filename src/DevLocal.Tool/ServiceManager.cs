using System.ComponentModel;
using System.Diagnostics;
using System.Security.Principal;

namespace DevLocal.Tool;

internal static class ServiceManager
{
    internal static async Task<int> RunAsync(string[] args, CancellationToken token)
    {
        if (!OperatingSystem.IsWindows())
            throw new PlatformNotSupportedException("Windows service commands require Windows");
        if (args.Length == 0)
            throw new ArgumentException("Expected service install, upgrade, uninstall, start, stop, restart, status, or logs");

        switch (args[0])
        {
            case "install":
                RequireAdmin();
                var settings = ParseSettings(args[1..]);
                if (!args.Any(arg => arg == "--docker-pipe" || arg.StartsWith("--docker-pipe=", StringComparison.Ordinal)))
                {
                    var pipe = await DockerContext.CurrentPipeAsync(token) ?? settings.DockerPipe;
                    settings = settings with { DockerPipe = pipe, Options = settings.Options! with { DockerPipe = pipe } };
                }
                if (await ExistsAsync(token))
                    throw new InvalidOperationException("DevLocal service already exists; use service upgrade");
                Deploy();
                await settings.SaveAsync(token);
                try
                {
                    var dllPath = Path.Combine(ServicePaths.ProgramDirectory, "DevLocal.Tool.dll");
                    await ScAsync(token, "create", ServicePaths.Name,
                        "binpath=", $"\"{DotnetPath()}\" \"{dllPath}\" service run",
                        "start=", "delayed-auto", "displayname=", "DevLocal");
                    await ScAsync(token, "failure", ServicePaths.Name, "reset=", "0", "actions=", "restart/60000/restart/60000/restart/60000");
                    await ScAsync(token, "failureflag", ServicePaths.Name, "1");
                    await ScAsync(token, "start", ServicePaths.Name);
                }
                catch
                {
                    if (await ExistsAsync(token))
                        await ScAsync(token, "delete", ServicePaths.Name);
                    throw;
                }
                return 0;
            case "upgrade":
                RequireAdmin();
                await RequireInstalledAsync(token);
                await StopAsync(token);
                Deploy();
                await ScAsync(token, "start", ServicePaths.Name);
                return 0;
            case "uninstall":
                RequireAdmin();
                await RequireInstalledAsync(token);
                await StopAsync(token);
                await ScAsync(token, "delete", ServicePaths.Name);
                Console.WriteLine($"Service removed. Configuration and logs remain in {ServicePaths.DataDirectory}.");
                return 0;
            case "start":
                RequireAdmin();
                await ScAsync(token, "start", ServicePaths.Name);
                return 0;
            case "stop":
                RequireAdmin();
                await StopAsync(token);
                return 0;
            case "restart":
                RequireAdmin();
                await StopAsync(token);
                await ScAsync(token, "start", ServicePaths.Name);
                return 0;
            case "status":
                Console.WriteLine(await ScAsync(token, "query", ServicePaths.Name));
                if (File.Exists(ServicePaths.ConfigPath) && (await ServiceSettings.LoadAsync(token)).ControllerEnabled)
                    Console.WriteLine("Mode: continuous controller");
                else if (File.Exists(ServicePaths.StatusPath))
                    Console.WriteLine(await File.ReadAllTextAsync(ServicePaths.StatusPath, token));
                return 0;
            case "logs":
                if (File.Exists(ServicePaths.LogPath))
                    foreach (var line in File.ReadLines(ServicePaths.LogPath).TakeLast(50))
                        Console.WriteLine(line);
                return 0;
            default:
                throw new ArgumentException($"Unknown service command: {args[0]}");
        }
    }

    internal static ServiceSettings ParseSettings(string[] args)
    {
        var controllerEnabled = args.Contains("--controller", StringComparer.Ordinal);
        var flags = args.Where(arg => arg != "--controller").ToArray();
        var options = CommandLine.Parse(flags, indexDir: Path.Combine(ServicePaths.DataDirectory, "ui")).Options;
        if (!Uri.TryCreate(options.CaddyAdmin, UriKind.Absolute, out var uri) || uri.Scheme != "http")
            throw new ArgumentException("--caddy-admin must be an HTTP URL");
        if (string.IsNullOrWhiteSpace(options.DockerPipe) || options.DockerPipe.Contains('\\') || options.DockerPipe.Contains('/'))
            throw new ArgumentException("--docker-pipe must be a pipe name, such as docker_engine");
        return new ServiceSettings(options.CaddyAdmin, options.DockerPipe)
        {
            Options = options,
            ControllerEnabled = controllerEnabled
        };
    }

    private static void RequireAdmin()
    {
        if (!OperatingSystem.IsWindows())
            throw new PlatformNotSupportedException();
        using var identity = WindowsIdentity.GetCurrent();
        if (!new WindowsPrincipal(identity).IsInRole(WindowsBuiltInRole.Administrator))
            throw new UnauthorizedAccessException("Run this command from an Administrator terminal");
    }

    private static string DotnetPath()
    {
        var root = Environment.GetEnvironmentVariable("DOTNET_ROOT");
        var path = Path.Combine(root ?? Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ProgramFiles), "dotnet"), "dotnet.exe");
        if (!File.Exists(path))
            throw new FileNotFoundException("Cannot locate dotnet.exe; set DOTNET_ROOT", path);
        return path;
    }

    private static void Deploy()
    {
        CopyRuntimeFiles(AppContext.BaseDirectory, ServicePaths.ProgramDirectory);
        if (!File.Exists(Path.Combine(ServicePaths.ProgramDirectory, "DevLocal.Tool.runtimeconfig.json")))
            throw new FileNotFoundException("Tool runtime files are missing from the installation");
    }

    internal static void CopyRuntimeFiles(string source, string destination)
    {
        Directory.CreateDirectory(destination);
        foreach (var file in Directory.EnumerateFiles(source, "*", SearchOption.AllDirectories))
        {
            if (!file.EndsWith(".dll", StringComparison.OrdinalIgnoreCase) &&
                !file.EndsWith(".json", StringComparison.OrdinalIgnoreCase))
                continue;
            var target = Path.Combine(destination, Path.GetRelativePath(source, file));
            Directory.CreateDirectory(Path.GetDirectoryName(target)!);
            File.Copy(file, target, true);
        }
    }

    private static async Task<bool> ExistsAsync(CancellationToken token)
    {
        var (_, exitCode) = await ScRawAsync(token, "query", ServicePaths.Name);
        return exitCode == 0;
    }

    private static async Task RequireInstalledAsync(CancellationToken token)
    {
        if (!await ExistsAsync(token))
            throw new InvalidOperationException("DevLocal service is not installed");
    }

    private static async Task StopAsync(CancellationToken token)
    {
        var state = await ScAsync(token, "query", ServicePaths.Name);
        if (state.Contains("STOPPED", StringComparison.Ordinal))
            return;
        await ScAsync(token, "stop", ServicePaths.Name);
        for (var i = 0; i < 60; i++)
        {
            await Task.Delay(500, token);
            state = await ScAsync(token, "query", ServicePaths.Name);
            if (state.Contains("STOPPED", StringComparison.Ordinal))
                return;
        }
        throw new TimeoutException("Service did not stop within 30 seconds");
    }

    private static async Task<string> ScAsync(CancellationToken token, params string[] args)
    {
        var (output, code) = await ScRawAsync(token, args);
        if (code != 0)
            throw new Win32Exception($"sc.exe {args[0]} failed ({code}): {output}");
        return output;
    }

    private static async Task<(string Output, int ExitCode)> ScRawAsync(CancellationToken token, params string[] args)
    {
        var start = new ProcessStartInfo(Path.Combine(Environment.SystemDirectory, "sc.exe"))
        {
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            UseShellExecute = false
        };
        foreach (var arg in args)
            start.ArgumentList.Add(arg);
        using var process = Process.Start(start) ?? throw new InvalidOperationException("Cannot start sc.exe");
        var stdout = process.StandardOutput.ReadToEndAsync(token);
        var stderr = process.StandardError.ReadToEndAsync(token);
        await process.WaitForExitAsync(token);
        return (await stdout + await stderr, process.ExitCode);
    }
}
