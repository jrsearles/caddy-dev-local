using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class GoHtmlTests
{
    [Theory]
    [InlineData("", "")]
    [InlineData("plain", "plain")]
    [InlineData("caf\u00e9", "caf\u00e9")]
    [InlineData("a&b", "a&amp;b")]
    [InlineData("<b>", "&lt;b&gt;")]
    [InlineData("\"q\"", "&#34;q&#34;")]
    [InlineData("it's", "it&#39;s")]
    [InlineData("a+b", "a&#43;b")]
    [InlineData("a\0b", "a\ufffdb")]
    public void EncodeMatchesGoReplacementTable(string input, string expected) =>
        Assert.Equal(expected, GoHtml.Encode(input));

    [Theory]
    [InlineData("https://web.dev.local", "https://web.dev.local")]
    [InlineData("https://a&b.dev.local", "https://a&amp;b.dev.local")]
    [InlineData("https://a+b.dev.local", "https://a&#43;b.dev.local")]
    [InlineData("https://a b.dev.local", "https://a%20b.dev.local")]
    [InlineData("https://a\"b.dev.local", "https://a%22b.dev.local")]
    [InlineData("https://a'b.dev.local", "https://a%27b.dev.local")]
    [InlineData("https://a<b>c.dev.local", "https://a%3cb%3ec.dev.local")]
    [InlineData("https://caf\u00e9.dev.local", "https://caf%c3%a9.dev.local")]
    [InlineData("https://a%2Fb.dev.local", "https://a%2Fb.dev.local")]
    [InlineData("https://a%zz.dev.local", "https://a%25zz.dev.local")]
    public void EncodeUrlNormalizesThenEscapes(string input, string expected) =>
        Assert.Equal(expected, GoHtml.EncodeUrl(input));

    [Fact]
    public void ElideCommentsRemovesHtmlAndScriptComments()
    {
        var source = "<!-- one -->\n<script>\n// line\nlet a = '//';\n/* block */\n/* multi\nline */\n</script>\n";
        var expected = "\n<script>\n\nlet a = '//';\n \n\n\n</script>\n";

        Assert.Equal(expected, GoHtml.ElideComments(source));
    }

    [Fact]
    public void ElideCommentsKeepsRegexAndDivision()
    {
        var source = "<script>\nlet a = String(x).replace(/\"/g, '\"');\nlet b = c / 2;\nlet d = /a\\/b/.test(c);\n</script>";

        Assert.Equal(source, GoHtml.ElideComments(source));
    }
}
