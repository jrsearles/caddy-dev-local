using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class IndexGoldenTests
{
    public static IEnumerable<object[]> Cases() => IndexCase.Load().Select(item => new object[] { item });

    [Theory]
    [MemberData(nameof(Cases))]
    public void MatchesGoOutput(IndexCase fixture)
    {
        var golden = File.ReadAllText(Path.Combine(AppContext.BaseDirectory, "Fixtures", "golden", "index",
            fixture.Slug + ".html"));

        Assert.Equal(Normalize(golden), Normalize(fixture.Render()));
    }

    private static string Normalize(string text) => text.Replace("\r\n", "\n", StringComparison.Ordinal);
}
