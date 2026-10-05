using System.Text.Json;

namespace DevLocal.Tool;

internal sealed record ControllerStatus(
    DateTimeOffset UpdatedAt,
    string Mode,
    string? UiBaseUrl,
    string[] Hooks,
    string[] Errors);

internal sealed class ControllerStatusReporter(string statusPath, string logPath)
{
    private const string UiPrefix = "Index UI served from ";

    private readonly object gate = new();
    private readonly List<string> errors = [];
    private string? uiBaseUrl;

    internal string? UiBaseUrl
    {
        get
        {
            lock (gate)
                return uiBaseUrl;
        }
    }

    internal void Announce(string message)
    {
        lock (gate)
        {
            if (message.StartsWith(UiPrefix, StringComparison.Ordinal))
                uiBaseUrl = message[UiPrefix.Length..];
        }
        Append(message);
    }

    internal void Record(string hook, Exception error)
    {
        lock (gate)
        {
            errors.Add($"{hook}: {error.Message}");
        }
        Append($"{hook} failed: {error.Message}");
    }

    internal async Task WriteAsync(IReadOnlyCollection<string> hooks, CancellationToken token)
    {
        ControllerStatus status;
        lock (gate)
        {
            status = new ControllerStatus(DateTimeOffset.UtcNow, "continuous controller",
                uiBaseUrl, hooks.Order(StringComparer.Ordinal).ToArray(), errors.ToArray());
        }
        Directory.CreateDirectory(Path.GetDirectoryName(statusPath)!);
        await File.WriteAllTextAsync(statusPath, JsonSerializer.Serialize(status), token);
    }

    private void Append(string message)
    {
        try
        {
            Directory.CreateDirectory(Path.GetDirectoryName(logPath)!);
            File.AppendAllText(logPath, message + Environment.NewLine);
        }
        catch (IOException)
        {
        }
        catch (UnauthorizedAccessException)
        {
        }
    }
}