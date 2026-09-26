using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class IndexParityTests
{
    public static IEnumerable<object[]> Cases() => IndexCase.Load().Select(item => new object[] { item });

    [Theory]
    [MemberData(nameof(Cases))]
    public void RendersSameFeaturesAsGo(IndexCase fixture)
    {
        var page = fixture.Render();

        foreach (var expected in fixture.ExpectedContains)
            Assert.Contains(expected, page);
        foreach (var excluded in fixture.ExcludedContains)
            Assert.DoesNotContain(excluded, page);
    }
}
