using System.Globalization;
using System.Text.RegularExpressions;

namespace DevLocal.Tool;

internal enum RunMode { Once, Start, Clean }

internal sealed record AppOptions
{
    public string Tld { get; init; } = "dev.local";
    public TimeSpan StaleTtl { get; init; } = TimeSpan.FromHours(1);
    public TimeSpan ProbeTimeout { get; init; } = TimeSpan.FromSeconds(2);
    public TimeSpan PollInterval { get; init; } = TimeSpan.FromSeconds(30);
    public bool HostsFile { get; init; } = true;
    public bool Tracing { get; init; } = true;
    public bool Caddy { get; init; } = true;
    public bool UI { get; init; } = true;
    public string CaddyAdmin { get; init; } = "http://localhost:2019";
    public string CaddyServer { get; init; } = "srv0";
    public bool AllowCreateServer { get; init; } = true;
    public string IndexDir { get; init; } = "";
    public string LogLevel { get; init; } = "info";
    public string DockerPipe { get; init; } = "docker_engine";
}

internal sealed record ParsedCommand(RunMode Mode, AppOptions Options);

internal static class CommandLine
{
    private static readonly Regex durationSegment = new(
        @"\G(?<amount>\d+(?:\.\d+)?)(?<unit>ns|us|µs|μs|ms|s|m|h)", RegexOptions.Compiled | RegexOptions.CultureInvariant);

    internal static ParsedCommand Parse(string[] args, Func<string, string?>? environment = null,
        string? indexDir = null, string dockerPipe = "docker_engine")
    {
        environment ??= Environment.GetEnvironmentVariable;
        string Env(string name, string fallback) => environment(name) is { Length: > 0 } value ? value : fallback;
        var options = new AppOptions
        {
            Tld = Env("DEVLOCAL_TLD", "dev.local"),
            StaleTtl = EnvDuration("DEVLOCAL_STALE_TTL", TimeSpan.FromHours(1)),
            ProbeTimeout = EnvDuration("DEVLOCAL_PROBE_TIMEOUT", TimeSpan.FromSeconds(2)),
            PollInterval = EnvDuration("DEVLOCAL_POLL_INTERVAL", TimeSpan.FromSeconds(30)),
            HostsFile = EnvBool("DEVLOCAL_HOSTS_FILE", true),
            Tracing = EnvBool("DEVLOCAL_TRACING", true),
            CaddyAdmin = Env("DEVLOCAL_CADDY_ADMIN", "http://localhost:2019"),
            CaddyServer = Env("DEVLOCAL_CADDY_SERVER", "srv0"),
            IndexDir = Env("DEVLOCAL_INDEX_DIR", indexDir ?? Path.Combine(
                Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "caddy-dev-local")),
            LogLevel = Env("DEVLOCAL_LOG_LEVEL", "info"),
            DockerPipe = dockerPipe
        };

        var mode = RunMode.Once;
        var cursor = 0;
        if (args.Length > 0 && !args[0].StartsWith('-'))
        {
            mode = args[0] switch
            {
                "start" => RunMode.Start,
                "clean" => RunMode.Clean,
                _ => throw new ArgumentException($"Unknown command {args[0]}")
            };
            cursor++;
        }

        for (; cursor < args.Length; cursor++)
        {
            var input = args[cursor];
            if (!input.StartsWith("--", StringComparison.Ordinal))
                throw new ArgumentException($"Unexpected argument {input}");
            var separator = input.IndexOf('=');
            var name = separator < 0 ? input[2..] : input[2..separator];
            var value = separator < 0 ? null : input[(separator + 1)..];
            var boolean = name is "hosts-file" or "caddy" or "ui" or "allow-create-server" or "no-tracing";
            if (boolean)
            {
                var enabled = value is null || ParseBool(value);
                options = name switch
                {
                    "hosts-file" => options with { HostsFile = enabled },
                    "caddy" => options with { Caddy = enabled },
                    "ui" => options with { UI = enabled },
                    "allow-create-server" => options with { AllowCreateServer = enabled },
                    "no-tracing" => options with { Tracing = !enabled },
                    _ => options
                };
                continue;
            }

            value ??= ++cursor < args.Length ? args[cursor] : throw new ArgumentException($"Missing value for --{name}");
            options = name switch
            {
                "tld" => options with { Tld = value },
                "stale-ttl" => options with { StaleTtl = ParseDuration(value) },
                "probe-timeout" => options with { ProbeTimeout = ParseDuration(value) },
                "poll-interval" => options with { PollInterval = ParseDuration(value) },
                "caddy-admin" => options with { CaddyAdmin = value },
                "caddy-server" => options with { CaddyServer = value },
                "index-dir" => options with { IndexDir = value },
                "log-level" => options with { LogLevel = value },
                "docker-pipe" => options with { DockerPipe = value },
                _ => throw new ArgumentException($"Unknown flag --{name}")
            };
        }

        if (options.IndexDir == "" || options.DockerPipe == "")
            throw new ArgumentException("Index directory and Docker pipe cannot be empty");
        if (options.LogLevel is not ("debug" or "info" or "warn" or "error"))
            throw new ArgumentException($"Invalid log level {options.LogLevel}");
        return new ParsedCommand(mode, options);

        bool EnvBool(string key, bool fallback) => environment(key) switch
        {
            "true" or "1" or "yes" => true,
            "false" or "0" or "no" => false,
            _ => fallback
        };
        TimeSpan EnvDuration(string key, TimeSpan fallback)
        {
            try { return environment(key) is { Length: > 0 } value ? ParseDuration(value) : fallback; }
            catch (FormatException) { return fallback; }
        }
    }

    internal static TimeSpan ParseDuration(string text)
    {
        if (text == "0")
            return TimeSpan.Zero;
        var negative = text.StartsWith('-');
        var position = negative || text.StartsWith('+') ? 1 : 0;
        decimal ticks = 0;
        var found = false;
        while (position < text.Length)
        {
            var match = durationSegment.Match(text, position);
            if (!match.Success || match.Index != position)
                throw new FormatException($"Invalid duration {text}");
            found = true;
            var amount = decimal.Parse(match.Groups["amount"].Value, CultureInfo.InvariantCulture);
            ticks += amount * (match.Groups["unit"].Value switch
            {
                "ns" => 0.01m,
                "us" or "µs" or "μs" => 10m,
                "ms" => TimeSpan.TicksPerMillisecond,
                "s" => TimeSpan.TicksPerSecond,
                "m" => TimeSpan.TicksPerMinute,
                "h" => TimeSpan.TicksPerHour,
                _ => throw new FormatException($"Invalid duration unit {text}")
            });
            position += match.Length;
        }
        if (!found)
            throw new FormatException($"Invalid duration {text}");
        try { return TimeSpan.FromTicks(decimal.ToInt64(decimal.Truncate(negative ? -ticks : ticks))); }
        catch (OverflowException ex) { throw new FormatException($"Duration out of range {text}", ex); }
    }

    private static bool ParseBool(string value) => value.ToLowerInvariant() switch
    {
        "true" or "1" or "t" => true,
        "false" or "0" or "f" => false,
        _ => throw new FormatException($"Invalid boolean value {value}")
    };
}
