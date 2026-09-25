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
            ["DEVLOCAL_HOSTS_FILE"] = "true",
            ["DEVLOCAL_TRACING"] = "false",
            ["DEVLOCAL_CADDY_ADMIN"] = "http://localhost:2020"
        };
        var parsed = CommandLine.Parse(
            ["start", "--tld", "flag.local", "--poll-interval=0", "--hosts-file=false", "--no-tracing=false", "--caddy=false"],
            name => env.GetValueOrDefault(name), "C:/index");

        Assert.Equal(RunMode.Start, parsed.Mode);
        Assert.Equal("flag.local", parsed.Options.Tld);
        Assert.Equal(TimeSpan.Zero, parsed.Options.PollInterval);
        Assert.False(parsed.Options.HostsFile);
        Assert.True(parsed.Options.Tracing);
        Assert.False(parsed.Options.Caddy);
        Assert.Equal("http://localhost:2020", parsed.Options.CaddyAdmin);
        Assert.Equal("C:/index", parsed.Options.IndexDir);
    }

    [Fact]
    public void KeepsGoDefaultsAndTreatsInvalidEnvironmentDurationAsDefault()
    {
        var parsed = CommandLine.Parse(["clean"], name => name == "DEVLOCAL_STALE_TTL" ? "invalid" : null, "C:/index");

        Assert.Equal(RunMode.Clean, parsed.Mode);
        Assert.Equal(TimeSpan.FromHours(1), parsed.Options.StaleTtl);
        Assert.Equal(TimeSpan.FromSeconds(2), parsed.Options.ProbeTimeout);
        Assert.True(parsed.Options.Caddy);
        Assert.True(parsed.Options.UI);
        Assert.True(parsed.Options.HostsFile);
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
    public void RejectsCommandsAfterFlagsAndInvalidExplicitDurations()
    {
        Assert.Throws<ArgumentException>(() => CommandLine.Parse(["--caddy=false", "start"], _ => null, "C:/index"));
        Assert.Throws<FormatException>(() => CommandLine.Parse(["--stale-ttl=broken"], _ => null, "C:/index"));
        Assert.Throws<ArgumentException>(() => CommandLine.Parse(["unknown"], _ => null, "C:/index"));
    }

    [Fact]
    public void ServiceOptionsPersistSelectedConfiguration()
    {
        var settings = ServiceManager.ParseSettings(["--controller", "--tld", "my.local", "--poll-interval", "0", "--hosts-file=false"]);
        var serialized = JsonSerializer.Serialize(settings);
        var restored = JsonSerializer.Deserialize<ServiceSettings>(serialized)!;

        Assert.Equal("my.local", restored.Options!.Tld);
        Assert.Equal(TimeSpan.Zero, restored.Options.PollInterval);
        Assert.False(restored.Options.HostsFile);
        Assert.True(restored.ControllerEnabled);
        Assert.Contains("\"Tld\":\"my.local\"", serialized);
    }
}
