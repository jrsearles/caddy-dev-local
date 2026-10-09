using System.Text.Json;
using System.Text.Json.Nodes;

namespace DevLocal.Ui;

public sealed class JsonTreeState
{
    private readonly HashSet<string> open = new(StringComparer.Ordinal);

    public bool IsOpen(string path) => open.Contains(path);

    public void Toggle(string path)
    {
        if (!open.Remove(path))
            open.Add(path);
    }

    public void Open(IEnumerable<string> paths)
    {
        foreach (var path in paths)
            open.Add(path);
    }

    public void CloseAll() => open.Clear();

    public void Reset() => open.Clear();

    public static IReadOnlyList<string> Paths(JsonNode? node, int maxDepth = int.MaxValue)
    {
        var paths = new List<string>();
        if (node is not null)
            paths.Add("");
        Walk(node, "", paths, 0, maxDepth);
        return paths;
    }

    private static void Walk(JsonNode? node, string path, List<string> paths, int depth, int maxDepth)
    {
        if (depth >= maxDepth)
            return;
        if (node is JsonObject obj)
        {
            foreach (var property in obj)
            {
                var child = path + "/" + property.Key;
                paths.Add(child);
                Walk(property.Value, child, paths, depth + 1, maxDepth);
            }
        }
        else if (node is JsonArray array)
        {
            for (var index = 0; index < array.Count; index++)
            {
                var child = path + "/" + index;
                paths.Add(child);
                Walk(array[index], child, paths, depth + 1, maxDepth);
            }
        }
    }
}

public static class JsonText
{
    public static string Pretty(string json) => Render(json, true);

    public static string Compact(string json) => Render(json, false);

    private static string Render(string json, bool indented)
    {
        if (string.IsNullOrEmpty(json))
            return "";
        try
        {
            using var document = JsonDocument.Parse(json);
            return JsonSerializer.Serialize(document.RootElement, new JsonSerializerOptions
            {
                WriteIndented = indented,
            });
        }
        catch (JsonException)
        {
            return json;
        }
    }

    public static JsonNode? Parse(string json)
    {
        if (string.IsNullOrEmpty(json))
            return null;
        try { return JsonNode.Parse(json); }
        catch (JsonException) { return null; }
    }
}