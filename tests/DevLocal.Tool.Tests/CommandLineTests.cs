using DevLocal.Tool;
using System.Text.Json;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class CommandLineTests
{
    [Fact]
    public void ExplicitFlagsOverrideEnvironmentIncludingZeroAndFalse()
    {
        var env = new Dictionary<string, string>
        {
            ["DEVLOCAL_TLD"] = "env.local",
            ["DEVLOCAL_POLL_INTERVAL"] = "30s",
            ["DEVLOCAL_TRACING"] = "false",
            ["DEVLOCAL_CADDY_ADMIN"] = "http://localhost:2020",
            ["DEVLOCAL_UI_PORT"] = "8080"
        };
        var parsed = CommandLine.Parse(
            ["run", "--tld", "flag.local", "--poll-interval=0", "--no-tracing=false"],
            name => env.GetValueOrDefault(name));

        Assert.Equal(RunMode.Start, parsed.Mode);
        Assert.Equal("flag.local", parsed.Options.Tld);
        Assert.Equal(TimeSpan.Zero, parsed.Options.PollInterval);
        Assert.True(parsed.Options.Tracing);
        Assert.Equal("http://localhost:2020", parsed.Options.CaddyAdmin);
        Assert.Equal(8080, parsed.Options.UiPort);
    }

    [Fact]
    public void KeepsGoDefaultsAndTreatsInvalidEnvironmentDurationAsDefault()
    {
        var parsed = CommandLine.Parse(["clean"], name => name == "DEVLOCAL_STALE_TTL" ? "invalid" : null);

        Assert.Equal(RunMode.Clean, parsed.Mode);
        Assert.Equal(TimeSpan.FromHours(1), parsed.Options.StaleTtl);
        Assert.Equal(TimeSpan.FromSeconds(2), parsed.Options.ProbeTimeout);
        Assert.Equal(0, parsed.Options.UiPort);
    }

    [Fact]
    public void ParsesUiPortFlagAndRejectsOutOfRangeValues()
    {
        Assert.Equal(5050, CommandLine.Parse(["run", "--ui-port", "5050"]).Options.UiPort);
        Assert.Equal(5050, CommandLine.Parse(["run", "--ui-port=5050"]).Options.UiPort);
        Assert.Throws<FormatException>(() => CommandLine.Parse(["run", "--ui-port=70000"]));
        Assert.Throws<FormatException>(() => CommandLine.Parse(["run", "--ui-port=abc"]));
    }

    [Fact]
    public void RejectsRemovedIndexDirFlag()
    {
        Assert.Throws<ArgumentException>(() => CommandLine.Parse(["run", "--index-dir", "C:/index"]));
    }

    [Theory]
    [InlineData("1h30m", 5400)]
    [InlineData("250ms", 0.25)]
    [InlineData("2s", 2)]
    [InlineData("0", 0)]
    [InlineData("-1m", -60)]
    public void ParsesGoStyleDurations(string value, double seconds) =>
        Assert.Equal(TimeSpan.FromSeconds(seconds), CommandLine.ParseDuration(value));

    [Fact]
    public void RejectsServiceStartAsForegroundCommandAndRequiresAnExplicitSubcommand()
    {
        Assert.Throws<ArgumentException>(() => CommandLine.Parse(["start"], _ => null));
        Assert.Throws<ArgumentException>(() => CommandLine.Parse(["--caddy=false"], _ => null));
        Assert.Throws<ArgumentException>(() => CommandLine.Parse(["--hosts-file=false", "--ui=false"], _ => null));
    }

    [Fact]
    public void ServiceCommandsAreRecognizedAtTopLevel()
    {
        Assert.True(ServiceManager.IsCommand("install"));
        Assert.True(ServiceManager.IsCommand("start"));
        Assert.False(ServiceManager.IsCommand("run"));
        Assert.False(ServiceManager.IsCommand("clean"));
    }

    [Fact]
    public void RejectsCommandsAfterFlagsAndInvalidExplicitDurations()
    {
        Assert.Throws<ArgumentException>(() => CommandLine.Parse(["run", "--caddy=false", "clean"], _ => null));
        Assert.Throws<FormatException>(() => CommandLine.Parse(["run", "--stale-ttl=broken"], _ => null));
        Assert.Throws<ArgumentException>(() => CommandLine.Parse(["unknown"], _ => null));
    }

    [Fact]
    public void ServiceOptionsPersistSelectedConfiguration()
    {
        var settings = ServiceManager.ParseSettings(["--tld", "my.local", "--poll-interval", "0", "--log-level=debug"]);
        var serialized = JsonSerializer.Serialize(settings);
        var restored = JsonSerializer.Deserialize<ServiceSettings>(serialized)!;

        Assert.Equal("my.local", restored.Options!.Tld);
        Assert.Equal(TimeSpan.Zero, restored.Options.PollInterval);
        Assert.Equal("debug", restored.Options.LogLevel);
        Assert.Contains("\"Tld\":\"my.local\"", serialized);
    }
}
