using System.Text;
using DevLocal.Core;

namespace DevLocal.Tool;

internal sealed class HostsFileReconciler(string path)
{
    private static readonly Encoding FileEncoding = new UTF8Encoding(false);

    internal static string WindowsHostsPath => Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.System), "drivers", "etc", "hosts");

    internal async Task SyncAsync(string tld, IEnumerable<string> domains, CancellationToken token)
    {
        var expected = HostsBlock.Build(tld, domains);
        var content = File.Exists(path) ? await File.ReadAllTextAsync(path, FileEncoding, token) : "";
        var existing = HostsBlock.Read(content);
        if (existing == expected)
            return;

        var replacement = existing != ""
            ? HostsBlock.Replace(content, expected)
            : content.TrimEnd('\r', '\n') is { Length: > 0 } trimmed
                ? trimmed + "\n" + expected + "\n"
                : expected + "\n";
        await File.WriteAllTextAsync(path, replacement, FileEncoding, token);
    }

    internal async Task RemoveAsync(CancellationToken token)
    {
        if (!File.Exists(path))
            return;

        var content = await File.ReadAllTextAsync(path, FileEncoding, token);
        if (HostsBlock.Read(content) is not { Length: > 0 })
            return;

        await File.WriteAllTextAsync(path, HostsBlock.Replace(content, ""), FileEncoding, token);
    }
}
