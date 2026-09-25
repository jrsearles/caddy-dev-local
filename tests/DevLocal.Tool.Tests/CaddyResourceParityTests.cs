using System.Collections.Immutable;
using System.Text.Json;
using System.Text.Json.Nodes;
using DevLocal.Core;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class CaddyResourceParityTests
{
    public sealed record CaddyCase(
        string Name,
        bool Tracing,
        Dictionary<string, string[]> Targets,
        Dictionary<string, JsonElement> ExpectedRoutes,
        Dictionary<string, JsonElement> ExpectedPolicies);

    public static IEnumerable<object[]> Cases()
    {
        var path = Path.Combine(AppContext.BaseDirectory, "Fixtures", "caddy_cases.json");
        var cases = JsonSerializer.Deserialize<CaddyCase[]>(File.ReadAllText(path),
            new JsonSerializerOptions { PropertyNameCaseInsensitive = true })!;
        return cases.Select(item => new object[] { item });
    }

    [Theory]
    [MemberData(nameof(Cases))]
    public void GeneratesSameRoutesAndPoliciesAsGo(CaddyCase fixture)
    {
        var targets = fixture.Targets.ToDictionary(
            item => item.Key, item => item.Value.ToImmutableArray(), StringComparer.Ordinal);
        var resources = CaddyResourceGenerator.Build(targets, fixture.Tracing);
        AssertResources(fixture.ExpectedRoutes, resources.Routes);
        AssertResources(fixture.ExpectedPolicies, resources.Policies);
        Assert.DoesNotContain("devlocal-index", resources.Routes.Keys);
    }

    private static void AssertResources(
        Dictionary<string, JsonElement> expected,
        IReadOnlyDictionary<string, JsonElement> actual)
    {
        Assert.Equal(expected.Keys.Order(StringComparer.Ordinal), actual.Keys.Order(StringComparer.Ordinal));
        foreach (var (id, resource) in expected)
            Assert.True(JsonNode.DeepEquals(JsonNode.Parse(resource.GetRawText()), JsonNode.Parse(actual[id].GetRawText())), id);
    }
}
