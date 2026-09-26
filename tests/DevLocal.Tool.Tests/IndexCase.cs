using System.Text.Json;
using DevLocal.Core;
using DevLocal.Tool;

namespace DevLocal.Tool.Tests;

public sealed record IndexCase(
    string Name,
    string Slug,
    string Tld,
    ContainerInfo[] Containers,
    string ConfigJSON,
    string DiscoveryError,
    long LastRefreshUnix,
    string[] ExpectedContains,
    string[] ExcludedContains)
{
    internal static IndexCase[] Load() =>
        JsonSerializer.Deserialize<IndexCase[]>(
            File.ReadAllText(Path.Combine(AppContext.BaseDirectory, "Fixtures", "index_cases.json")),
            new JsonSerializerOptions { PropertyNameCaseInsensitive = true })!;

    internal DiscoveryUpdate Update() => new([.. Containers],
        new DiscoveryStatus(LastRefreshUnix == 0 ? null : DateTimeOffset.FromUnixTimeSeconds(LastRefreshUnix),
            DiscoveryError));

    internal string Render() => IndexTemplate.Render(IndexViewModelBuilder.Build(Tld, Update(), ConfigJSON));
}
