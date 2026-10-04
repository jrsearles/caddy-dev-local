using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class ToolVersionTests
{
    [Fact]
    public void ShortensCommitMetadataAndKeepsSemver()
    {
        Assert.Equal("0.1.0-preview.10 (be3c16a)",
            ToolVersion.Format("0.1.0-preview.10+be3c16a51a7a0128a20ce72d944a578475b83ec5"));
    }

    [Theory]
    [InlineData("1.0.0", "1.0.0")]
    [InlineData("1.0.0+", "1.0.0")]
    [InlineData("1.0.0+abc", "1.0.0 (abc)")]
    [InlineData(null, "unknown")]
    [InlineData("", "unknown")]
    [InlineData("   ", "unknown")]
    public void FormatsEdgeCases(string? informational, string expected) =>
        Assert.Equal(expected, ToolVersion.Format(informational));

    [Fact]
    public void ReportsAVersionForTheRunningAssembly() =>
        Assert.NotEqual("unknown", ToolVersion.Display);
}