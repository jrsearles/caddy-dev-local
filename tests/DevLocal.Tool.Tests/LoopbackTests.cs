using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class LoopbackTests
{
    [Theory]
    [InlineData("http://localhost:2019", "http://127.0.0.1:2019")]
    [InlineData("http://LOCALHOST:2019", "http://127.0.0.1:2019")]
    [InlineData("http://localhost:2019/", "http://127.0.0.1:2019")]
    [InlineData("http://localhost:2019/admin", "http://127.0.0.1:2019/admin")]
    [InlineData("http://127.0.0.1:2019", "http://127.0.0.1:2019")]
    [InlineData("https://localhost:2019", "https://localhost:2019")]
    [InlineData("http://dev.local:2019", "http://dev.local:2019")]
    [InlineData("localhost:2019", "localhost:2019")]
    public void NormalizeUrl_RewritesOnlyHttpLoopbackHost(string input, string expected)
    {
        Assert.Equal(expected, Loopback.NormalizeUrl(input));
    }
}