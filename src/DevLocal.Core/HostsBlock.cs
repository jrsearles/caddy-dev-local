using System.Text;

namespace DevLocal.Core;

public static class HostsBlock
{
    public const string BeginMarker = "# dev-local:BEGIN";
    public const string EndMarker = "# dev-local:END";

    public static string Build(string tld, IEnumerable<string> domains)
    {
        var sorted = domains.OrderBy(domain => domain, StringComparer.Ordinal).ToArray();
        var block = new StringBuilder()
            .Append(BeginMarker).Append('\n')
            .Append("# Managed by caddy-dev-local — do not edit.\n");

        var uiHost = DomainGenerator.TldLocalhost(tld);
        if (!sorted.Contains(uiHost, StringComparer.Ordinal))
            block.Append("127.0.0.1    ").Append(uiHost).Append('\n');

        foreach (var domain in sorted)
            block.Append("127.0.0.1    ").Append(domain).Append('\n');

        return block.Append(EndMarker).Append('\n').ToString();
    }

    public static string Read(string content)
    {
        var lines = new List<string>();
        var inBlock = false;
        foreach (var line in content.Split('\n'))
        {
            var trimmed = line.Trim();
            if (trimmed == BeginMarker)
            {
                inBlock = true;
                lines.Add(line);
            }
            else if (trimmed == EndMarker)
            {
                inBlock = false;
                lines.Add(line);
            }
            else if (inBlock)
            {
                lines.Add(line);
            }
        }

        return lines.Count == 0 ? "" : string.Join('\n', lines) + "\n";
    }

    public static string Replace(string content, string block)
    {
        var lines = new List<string>();
        var inBlock = false;
        foreach (var line in content.Split('\n'))
        {
            var trimmed = line.Trim();
            if (trimmed == BeginMarker)
            {
                inBlock = true;
                if (block != "")
                    lines.Add(block.TrimEnd('\n'));
            }
            else if (trimmed == EndMarker)
            {
                inBlock = false;
            }
            else if (!inBlock)
            {
                lines.Add(line);
            }
        }

        var output = string.Join('\n', lines).TrimEnd('\n');
        return output == "" ? "" : output + "\n";
    }
}
