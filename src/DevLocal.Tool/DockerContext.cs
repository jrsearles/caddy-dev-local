using System.Diagnostics;
using System.Text.Json;

namespace DevLocal.Tool;

internal static class DockerContext
{
    internal static string? PipeFromInspect(string json)
    {
        using var document = JsonDocument.Parse(json);
        var contexts = document.RootElement;
        if (contexts.ValueKind != JsonValueKind.Array || contexts.GetArrayLength() == 0)
            return null;
        var host = contexts[0].GetProperty("Endpoints").GetProperty("docker").GetProperty("Host").GetString();
        const string prefix = "npipe:////./pipe/";
        if (host is null || !host.StartsWith(prefix, StringComparison.OrdinalIgnoreCase))
            return null;
        var pipe = host[prefix.Length..];
        return pipe.Length > 0 && !pipe.Contains('/') && !pipe.Contains('\\') ? pipe : null;
    }

    internal static async Task<string?> CurrentPipeAsync(CancellationToken token)
    {
        try
        {
            var start = new ProcessStartInfo("docker")
            {
                RedirectStandardOutput = true,
                RedirectStandardError = true,
                UseShellExecute = false
            };
            start.ArgumentList.Add("context");
            start.ArgumentList.Add("inspect");
            using var process = Process.Start(start);
            if (process is null)
                return null;
            var output = process.StandardOutput.ReadToEndAsync(token);
            var errors = process.StandardError.ReadToEndAsync(token);
            await process.WaitForExitAsync(token);
            _ = await errors;
            return process.ExitCode == 0 ? PipeFromInspect(await output) : null;
        }
        catch (Exception) when (!token.IsCancellationRequested)
        {
            return null;
        }
    }
}
