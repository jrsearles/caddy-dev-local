using System.ComponentModel;
using System.Diagnostics;
using System.Security.Principal;
using System.Text.Json;
using System.Text.RegularExpressions;
using Spectre.Console;

namespace DevLocal.Tool;

internal static class ServiceManager
{
    private static readonly HashSet<string> commands = new(StringComparer.Ordinal)
    {
        "install", "upgrade", "uninstall", "start", "stop", "restart", "status", "logs"
    };

    internal static bool IsCommand(string name) => commands.Contains(name);

    internal static async Task<int> RunAsync(string[] args, CancellationToken token)
    {
        if (!OperatingSystem.IsWindows())
            throw new PlatformNotSupportedException("Windows service commands require Windows");
        if (args.Length == 0 || !IsCommand(args[0]))
            throw new ArgumentException("Expected install, upgrade, uninstall, start, stop, restart, status, or logs");

        switch (args[0])
        {
            case "install":
                RequireAdmin();
                var settings = ParseSettings(args[1..]);
                if (await ExistsAsync(token))
                    throw new InvalidOperationException("DevLocal service already exists; use devlocal upgrade");
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
                Cli.Out.MarkupLine("[green]✓[/] Service installed and started. Check [aqua]devlocal status[/] or [aqua]devlocal logs[/].");
                return 0;
            case "upgrade":
                RequireAdmin();
                await RequireInstalledAsync(token);
                await StopAsync(token);
                Deploy();
                await ScAsync(token, "start", ServicePaths.Name);
                Cli.Out.MarkupLine("[green]✓[/] Service upgraded and started.");
                return 0;
            case "uninstall":
                RequireAdmin();
                await RequireInstalledAsync(token);
                await StopAsync(token);
                await CleanAsync(token);
                await ScAsync(token, "delete", ServicePaths.Name);
                Cli.Out.MarkupLineInterpolated($"[green]✓[/] Service removed and managed routes, TLS policies and hosts entries cleaned. Configuration and logs remain in {Markup.Escape(ServicePaths.DataDirectory)}.");
                return 0;
            case "start":
                RequireAdmin();
                await RequireInstalledAsync(token);
                await ScAsync(token, "start", ServicePaths.Name);
                Cli.Out.MarkupLine("[green]✓[/] Service started.");
                return 0;
            case "stop":
                RequireAdmin();
                await RequireInstalledAsync(token);
                await StopAsync(token);
                Cli.Out.MarkupLine("[green]✓[/] Service stopped.");
                return 0;
            case "restart":
                RequireAdmin();
                await RequireInstalledAsync(token);
                await StopAsync(token);
                await ScAsync(token, "start", ServicePaths.Name);
                Cli.Out.MarkupLine("[green]✓[/] Service restarted.");
                return 0;
            case "status":
                await PrintStatusAsync(token);
                return 0;
            case "logs":
                if (File.Exists(ServicePaths.ControllerLogPath))
                    foreach (var line in File.ReadLines(ServicePaths.ControllerLogPath).TakeLast(50))
                        Console.WriteLine(line);
                else
                    Cli.Out.MarkupLineInterpolated($"[yellow]No log output yet[/]; the service has not written to {Markup.Escape(ServicePaths.ControllerLogPath)}.");
                return 0;
            default:
                throw new ArgumentException($"Unknown service command: {args[0]}");
        }
    }

    internal static async Task PrintStatusAsync(CancellationToken token)
    {
        if (!await ExistsAsync(token))
        {
            Cli.Out.MarkupLine("Service: [red]not installed[/]. Run [aqua]devlocal install[/] from an Administrator terminal.");
            return;
        }
        var state = DescribeState(await ScAsync(token, "query", ServicePaths.Name));
        var colored = state switch
        {
            "running" => $"[green]{state}[/]",
            "stopped" or "starting" or "stopping" or "paused" => $"[yellow]{state}[/]",
            _ => $"[red]{state}[/]"
        };
        Cli.Out.MarkupLine($"Service: installed, {colored}");
        if (!File.Exists(ServicePaths.ControllerStatusPath))
        {
            Cli.Out.MarkupLine("Controller: [yellow]no status yet[/]; the service may not have finished starting.");
            return;
        }
        try
        {
            var status = JsonSerializer.Deserialize<ControllerStatus>(
                await File.ReadAllTextAsync(ServicePaths.ControllerStatusPath, token));
            if (status is null)
                throw new JsonException("Status is empty");
            Cli.Out.MarkupLine($"Controller: last refreshed {status.UpdatedAt.ToLocalTime():yyyy-MM-dd HH:mm:ss}");
            Cli.Out.MarkupLine($"Index UI: {(string.IsNullOrEmpty(status.UiBaseUrl) ? "[dim]not started[/]" : Markup.Escape(status.UiBaseUrl))}");
            Cli.Out.MarkupLine($"Hooks: {(status.Hooks.Length == 0 ? "[dim]none[/]" : Markup.Escape(string.Join(", ", status.Hooks)))}");
            foreach (var error in status.Errors)
                Cli.Out.MarkupLine($"[red]Error:[/] {Markup.Escape(error)}");
        }
        catch (JsonException)
        {
            Cli.Out.MarkupLine("Controller: [red]status could not be read[/]; the service may be upgrading.");
        }
    }

    private static string DescribeState(string query)
    {
        var match = Regex.Match(query, @"STATE\s*:\s*\d+\s+([A-Z_]+)");
        return match.Success
            ? match.Groups[1].Value switch
            {
                "RUNNING" => "running",
                "STOPPED" => "stopped",
                "START_PENDING" => "starting",
                "STOP_PENDING" => "stopping",
                "PAUSED" => "paused",
                _ => match.Groups[1].Value.ToLowerInvariant()
            }
            : "state unknown";
    }

    internal static ServiceSettings ParseSettings(string[] args)
    {
        var options = CommandLine.ParseOptions(args);
        if (!Uri.TryCreate(options.CaddyAdmin, UriKind.Absolute, out var uri) || uri.Scheme != "http")
            throw new ArgumentException("--caddy-admin must be an HTTP URL");
        return new ServiceSettings(options.CaddyAdmin)
        {
            Options = options
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
            var relative = Path.GetRelativePath(source, file);
            var isWebAsset = relative.StartsWith("wwwroot" + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase);
            if (!isWebAsset &&
                !file.EndsWith(".dll", StringComparison.OrdinalIgnoreCase) &&
                !file.EndsWith(".json", StringComparison.OrdinalIgnoreCase))
                continue;
            var target = Path.Combine(destination, relative);
            Directory.CreateDirectory(Path.GetDirectoryName(target)!);
            File.Copy(file, target, true);
        }
    }

    private static async Task CleanAsync(CancellationToken token)
    {
        ServiceSettings settings;
        try
        {
            settings = await ServiceSettings.LoadAsync(token);
        }
        catch (Exception ex) when (ex is IOException or InvalidDataException or JsonException)
        {
            settings = ServiceSettings.Default;
        }
        await using var session = await ControllerFactory.Create(settings.Options ?? new AppOptions());
        try
        {
            await session.Controller.CleanupAsync(token);
        }
        catch (Exception ex) when (!token.IsCancellationRequested)
        {
            Cli.Err.MarkupLine($"[yellow]devlocal:[/] cleanup failed: {Markup.Escape(ex.Message)};");
            Cli.Err.MarkupLine("Managed routes or hosts entries may remain and can be removed by running [aqua]devlocal uninstall[/] again.");
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
