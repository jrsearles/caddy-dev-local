using System.Collections.Immutable;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using DevLocal.Core;

namespace DevLocal.Tool;

internal sealed class UiHook(string tld, string outputDir, ICaddyConfigClient? caddy) : IUpdateHook
{
    private readonly SemaphoreSlim gate = new(1, 1);
    private string cachedConfig = "";

    public string Name => "ui";

    public async Task ApplyAsync(DiscoveryUpdate update, CancellationToken token)
    {
        await gate.WaitAsync(token);
        try
        {
            if (outputDir == "")
                throw new ArgumentException("UI output directory is empty", nameof(outputDir));

            if (caddy is not null)
            {
                try
                {
                    cachedConfig = await caddy.RunningConfigAsync(token);
                }
                catch (Exception) when (!token.IsCancellationRequested)
                {
                }
            }

            var page = IndexTemplate.Render(IndexViewModelBuilder.Build(tld, update, cachedConfig));
            var css = IndexTemplate.Css;
            var version = Fingerprint(page, css);
            Directory.CreateDirectory(outputDir);
            if (!OperatingSystem.IsWindows())
                File.SetUnixFileMode(outputDir, UnixFileMode.UserRead | UnixFileMode.UserWrite | UnixFileMode.UserExecute |
                    UnixFileMode.GroupRead | UnixFileMode.GroupExecute | UnixFileMode.OtherRead | UnixFileMode.OtherExecute);
            await WriteIfChangedAsync(Path.Combine(outputDir, "index.html"), Encoding.UTF8.GetBytes(page), token);
            await WriteIfChangedAsync(Path.Combine(outputDir, "index.css"), Encoding.UTF8.GetBytes(css), token);
            await WriteIfChangedAsync(Path.Combine(outputDir, "version.json"),
                JsonSerializer.SerializeToUtf8Bytes(new { v = version }), token);

            if (caddy is not null)
                await caddy.ReconcileAsync(IndexResources(update.Snapshot), Owns, token);
        }
        finally
        {
            gate.Release();
        }
    }

    public async Task CleanupAsync(CancellationToken token)
    {
        await gate.WaitAsync(token);
        try
        {
            if (outputDir == "")
                throw new ArgumentException("UI output directory is empty", nameof(outputDir));
            foreach (var name in new[] { "index.html", "index.css", "version.json" })
            {
                var path = Path.Combine(outputDir, name);
                if (File.Exists(path))
                    File.Delete(path);
            }
            if (caddy is not null)
                await caddy.CleanupAsync(token);
        }
        finally
        {
            gate.Release();
        }
    }

    private static bool Owns(string id) => id is "devlocal-index" or "devlocal-tls-ui";

    private CaddyResources IndexResources(ImmutableArray<ContainerInfo> containers)
    {
        var reserved = DomainGenerator.DomainTargets(tld, containers).Keys.ToHashSet(StringComparer.Ordinal);
        var hosts = new[] { tld, DomainGenerator.TldLocalhost(tld) }
            .Where(host => !reserved.Contains(host)).Distinct(StringComparer.Ordinal).ToArray();
        var routes = ImmutableDictionary.CreateBuilder<string, JsonElement>(StringComparer.Ordinal);
        var policies = ImmutableDictionary.CreateBuilder<string, JsonElement>(StringComparer.Ordinal);
        if (hosts.Length != 0)
        {
            routes["devlocal-index"] = JsonSerializer.SerializeToElement(new Dictionary<string, object>
            {
                ["@id"] = "devlocal-index",
                ["handle"] = new object[] { new Dictionary<string, object>
                {
                    ["handler"] = "subroute",
                    ["routes"] = new object[] { new Dictionary<string, object>
                    {
                        ["handle"] = new object[]
                        {
                            new Dictionary<string, object> { ["handler"] = "vars", ["root"] = outputDir },
                            new Dictionary<string, object> { ["handler"] = "file_server", ["hide"] = new[] { "./Caddyfile" } }
                        }
                    } }
                } },
                ["match"] = new object[] { new Dictionary<string, object> { ["host"] = hosts } },
                ["terminal"] = true
            });
            policies["devlocal-tls-ui"] = JsonSerializer.SerializeToElement(new Dictionary<string, object>
            {
                ["@id"] = "devlocal-tls-ui",
                ["issuers"] = new object[] { new Dictionary<string, object> { ["module"] = "internal" } },
                ["subjects"] = hosts
            });
        }
        return new CaddyResources(routes.ToImmutable(), policies.ToImmutable());
    }

    private static string Fingerprint(params string[] parts)
    {
        using var hash = IncrementalHash.CreateHash(HashAlgorithmName.SHA256);
        foreach (var part in parts)
        {
            hash.AppendData(Encoding.UTF8.GetBytes(part));
            hash.AppendData([0]);
        }
        return Convert.ToHexStringLower(hash.GetHashAndReset());
    }

    private static async Task WriteIfChangedAsync(string path, byte[] content, CancellationToken token)
    {
        if (File.Exists(path) && (await File.ReadAllBytesAsync(path, token)).AsSpan().SequenceEqual(content))
        {
            SetPermissions(path);
            return;
        }

        var temp = Path.Combine(Path.GetDirectoryName(path)!, ".devlocal-" + Guid.NewGuid().ToString("N"));
        try
        {
            await using (var stream = new FileStream(temp, FileMode.CreateNew, FileAccess.Write, FileShare.None))
            {
                await stream.WriteAsync(content, token);
                stream.Flush(true);
            }
            SetPermissions(temp);
            File.Move(temp, path, overwrite: true);
        }
        finally
        {
            if (File.Exists(temp))
                File.Delete(temp);
        }
    }

    private static void SetPermissions(string path)
    {
        if (!OperatingSystem.IsWindows())
            File.SetUnixFileMode(path, UnixFileMode.UserRead | UnixFileMode.UserWrite |
                UnixFileMode.GroupRead | UnixFileMode.OtherRead);
    }
}
