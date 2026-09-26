using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class GoJsonTests
{
    [Fact]
    public void FromLabelsSortsKeysAndMatchesGoEscaping()
    {
        var labels = new Dictionary<string, string>(StringComparer.Ordinal)
        {
            ["org.opencontainers.image.b"] = "two",
            ["org.opencontainers.image.a"] = "<one> & 'two'"
        };

        Assert.Equal("{\"org.opencontainers.image.a\":\"\\u003cone\\u003e \\u0026 'two'\","
            + "\"org.opencontainers.image.b\":\"two\"}", GoJson.FromLabels(labels));
    }

    [Fact]
    public void CanonicalizeSortsNestedObjectsAndFormatsNumbers()
    {
        const string json = "{\"b\":1,\"a\":{\"z\":[3,2,1],\"y\":1e21,\"x\":9007199254740993}}";

        Assert.Equal("{\"a\":{\"x\":9007199254740993,\"y\":1e+21,\"z\":[3,2,1]},\"b\":1}",
            GoJson.Canonicalize(json));
    }

    [Fact]
    public void CanonicalizeEscapesMarkupLikeGoAndKeepsInvalidJson()
    {
        Assert.Equal("{\"a\":\"\\u003c/script\\u003e \\u0026 \\\"q\\\"\"}",
            GoJson.Canonicalize("{\"a\":\"</script> & \\\"q\\\"\"}"));
        Assert.Equal("{not json \\u003cb\\u003e \\u0026}", GoJson.Canonicalize("{not json <b> &}"));
    }
}
