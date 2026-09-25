using System.Text.Json;
using DevLocal.Core;
using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class IndexParityTests
{
    public sealed record IndexCase(
        string Name,
        string Tld,
        ContainerInfo[] Containers,
        string ConfigJSON,
        string DiscoveryError,
        string[] ExpectedContains,
        string[] ExcludedContains);

    public static IEnumerable<object[]> Cases()
    {
        var path = Path.Combine(AppContext.BaseDirectory, "Fixtures", "index_cases.json");
        var cases = JsonSerializer.Deserialize<IndexCase[]>(File.ReadAllText(path),
            new JsonSerializerOptions { PropertyNameCaseInsensitive = true })!;
        return cases.Select(item => new object[] { item });
    }

    [Theory]
    [MemberData(nameof(Cases))]
    public void RendersSameFeaturesAsGo(IndexCase fixture)
    {
        var update = new DiscoveryUpdate([.. fixture.Containers], new DiscoveryStatus(null, fixture.DiscoveryError));
        var page = IndexTemplate.Render(IndexViewModelBuilder.Build(fixture.Tld, update, fixture.ConfigJSON));

        foreach (var expected in fixture.ExpectedContains)
            Assert.Contains(expected, page);
        foreach (var excluded in fixture.ExcludedContains)
            Assert.DoesNotContain(excluded, page);
    }
}
