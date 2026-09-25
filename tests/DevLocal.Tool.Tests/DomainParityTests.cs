using System.Text.Json;
using DevLocal.Core;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class DomainParityTests
{
    public sealed record DomainCase(
        string Name,
        string Tld,
        ContainerInfo[] Containers,
        string[] ExpectedDomains,
        Dictionary<string, string[]> ExpectedTargets);

    public static IEnumerable<object[]> Cases()
    {
        var path = Path.Combine(AppContext.BaseDirectory, "Fixtures", "domain_cases.json");
        var json = File.ReadAllText(path);
        var cases = JsonSerializer.Deserialize<DomainCase[]>(json,
            new JsonSerializerOptions { PropertyNameCaseInsensitive = true })!;
        return cases.Select(item => new object[] { item });
    }

    [Theory]
    [MemberData(nameof(Cases))]
    public void GeneratesSameDomainsAndTargetsAsGo(DomainCase fixture)
    {
        var domains = DomainGenerator.Domains(fixture.Tld, fixture.Containers);
        var targets = DomainGenerator.DomainTargets(fixture.Tld, fixture.Containers);

        Assert.Equal(fixture.ExpectedDomains, domains);
        Assert.Equal(fixture.ExpectedTargets.Keys.Order(StringComparer.Ordinal), targets.Keys.Order(StringComparer.Ordinal));
        foreach (var (domain, expected) in fixture.ExpectedTargets)
            Assert.Equal(expected, targets[domain]);
    }

    [Theory]
    [InlineData("dev.local", "dev.localhost")]
    [InlineData("my.dev.local", "my.localhost")]
    [InlineData("localhost", "localhost")]
    [InlineData("DEV.LOCALHOST", "DEV.LOCALHOST")]
    public void MatchesGoTldAlias(string tld, string expected) =>
        Assert.Equal(expected, DomainGenerator.TldLocalhost(tld));
}
