using System.Reflection;

namespace DevLocal.Tool;

internal static class ToolVersion
{
    internal static string Display { get; } = Format(
        typeof(ToolVersion).Assembly.GetCustomAttribute<AssemblyInformationalVersionAttribute>()?.InformationalVersion);

    internal static string Format(string? informational)
    {
        if (string.IsNullOrWhiteSpace(informational))
            return "unknown";

        var parts = informational.Split('+', 2);
        if (parts.Length == 2 && parts[1].Length > 0)
        {
            var commit = parts[1];
            return $"{parts[0]} ({commit[..Math.Min(commit.Length, 7)]})";
        }
        return parts[0];
    }
}