using System.Text;

namespace DevLocal.Tool;

internal static class GoHtml
{
    private const string Hex = "0123456789abcdef";

    internal static string Encode(string? text)
    {
        if (string.IsNullOrEmpty(text)) return text ?? "";
        StringBuilder? builder = null;
        for (var i = 0; i < text.Length; i++)
        {
            var replacement = Replacement(text[i]);
            if (replacement is null)
            {
                builder?.Append(text[i]);
                continue;
            }
            builder ??= new StringBuilder().Append(text, 0, i);
            builder.Append(replacement);
        }
        return builder?.ToString() ?? text;
    }

    internal static string EncodeUrl(string? url)
    {
        if (string.IsNullOrEmpty(url)) return url ?? "";
        return Encode(NormalizeUrl(url));
    }

    private static string? Replacement(char c) => c switch
    {
        '\0' => "\uFFFD",
        '"' => "&#34;",
        '&' => "&amp;",
        '\'' => "&#39;",
        '+' => "&#43;",
        '<' => "&lt;",
        '>' => "&gt;",
        _ => null
    };

    private static string NormalizeUrl(string url)
    {
        var bytes = Encoding.UTF8.GetBytes(url);
        StringBuilder? builder = null;
        for (var i = 0; i < bytes.Length; i++)
        {
            var current = bytes[i];
            if (Keep(bytes, i))
            {
                builder?.Append((char)current);
                continue;
            }
            if (builder is null)
            {
                builder = new StringBuilder(url.Length);
                builder.Append(url, 0, i);
            }
            builder.Append('%').Append(Hex[current >> 4]).Append(Hex[current & 0x0F]);
        }
        return builder?.ToString() ?? url;
    }

    private static bool Keep(byte[] bytes, int index)
    {
        var current = bytes[index];
        switch (current)
        {
            case (byte)'!':
            case (byte)'#':
            case (byte)'$':
            case (byte)'&':
            case (byte)'*':
            case (byte)'+':
            case (byte)',':
            case (byte)'/':
            case (byte)':':
            case (byte)';':
            case (byte)'=':
            case (byte)'?':
            case (byte)'@':
            case (byte)'[':
            case (byte)']':
            case (byte)'-':
            case (byte)'.':
            case (byte)'_':
            case (byte)'~':
                return true;
            case (byte)'%':
                return index + 2 < bytes.Length && IsHex(bytes[index + 1]) && IsHex(bytes[index + 2]);
            default:
                return current is >= (byte)'a' and <= (byte)'z'
                    or >= (byte)'A' and <= (byte)'Z'
                    or >= (byte)'0' and <= (byte)'9';
        }
    }

    private static bool IsHex(byte value) =>
        value is >= (byte)'0' and <= (byte)'9'
            or >= (byte)'a' and <= (byte)'f'
            or >= (byte)'A' and <= (byte)'F';

    internal static string ElideComments(string source)
    {
        var output = new StringBuilder(source.Length);
        var index = 0;
        var inScript = false;
        var previous = default(char);
        string? word = null;
        while (index < source.Length)
        {
            if (inScript)
            {
                if (Match(source, index, "</script"))
                {
                    inScript = false;
                    previous = default;
                    word = null;
                    output.Append(source, index, 8);
                    index += 8;
                    continue;
                }
                var current = source[index];
                if (current is '\'' or '"')
                {
                    index = CopyQuoted(output, source, index);
                    previous = current;
                    word = null;
                    continue;
                }
                if (current == '/' && index + 1 < source.Length)
                {
                    if (source[index + 1] == '/')
                    {
                        index = SkipLineComment(source, index + 2);
                        continue;
                    }
                    if (source[index + 1] == '*')
                    {
                        index = SkipBlockComment(output, source, index + 2);
                        continue;
                    }
                    if (ExpectsRegex(previous, word))
                    {
                        index = CopyRegex(output, source, index);
                        previous = '/';
                        word = null;
                        continue;
                    }
                }
                if (char.IsWhiteSpace(current))
                {
                    output.Append(current);
                    index++;
                    continue;
                }
                output.Append(current);
                index++;
                if (char.IsLetterOrDigit(current) || current is '_' or '$')
                {
                    var start = index - 1;
                    while (index < source.Length && (char.IsLetterOrDigit(source[index]) || source[index] is '_' or '$'))
                    {
                        output.Append(source[index]);
                        index++;
                    }
                    word = source[start..index];
                }
                else
                {
                    previous = current;
                    word = null;
                }
                continue;
            }
            if (Match(source, index, "<!--"))
            {
                var end = source.IndexOf("-->", index + 4, StringComparison.Ordinal);
                index = end < 0 ? source.Length : end + 3;
                continue;
            }
            if (Match(source, index, "<script"))
            {
                inScript = true;
                previous = default;
                word = null;
                output.Append(source, index, 7);
                index += 7;
                continue;
            }
            output.Append(source[index]);
            index++;
        }
        return output.ToString();
    }

    private static readonly string[] RegexKeywords =
        ["await", "case", "delete", "do", "else", "in", "instanceof", "new", "of", "return", "throw",
            "typeof", "void", "yield"];

    private static readonly string RegexPrefixes = "([{,;:=+-*%&|!?~^<>}";

    private static bool ExpectsRegex(char previous, string? word) =>
        word is not null
            ? Array.IndexOf(RegexKeywords, word) >= 0
            : RegexPrefixes.IndexOf(previous) >= 0;

    private static bool Match(string source, int index, string value) =>
        index + value.Length <= source.Length &&
        string.CompareOrdinal(source, index, value, 0, value.Length) == 0;

    private static int CopyQuoted(StringBuilder output, string source, int index)
    {
        var quote = source[index];
        output.Append(quote);
        for (var i = index + 1; i < source.Length; i++)
        {
            output.Append(source[i]);
            if (source[i] == '\\' && i + 1 < source.Length)
            {
                output.Append(source[i + 1]);
                i++;
                continue;
            }
            if (source[i] == quote) return i + 1;
        }
        return source.Length;
    }

    private static int CopyRegex(StringBuilder output, string source, int index)
    {
        output.Append(source[index]);
        var inClass = false;
        for (var i = index + 1; i < source.Length; i++)
        {
            output.Append(source[i]);
            if (source[i] == '\\' && i + 1 < source.Length)
            {
                output.Append(source[i + 1]);
                i++;
                continue;
            }
            if (source[i] is '\n' or '\r') return i;
            if (source[i] == '[') inClass = true;
            else if (source[i] == ']') inClass = false;
            else if (source[i] == '/' && !inClass) return i + 1;
        }
        return source.Length;
    }

    private static int SkipLineComment(string source, int index)
    {
        while (index < source.Length && source[index] is not ('\n' or '\r')) index++;
        return index;
    }

    private static int SkipBlockComment(StringBuilder output, string source, int index)
    {
        var end = source.IndexOf("*/", index, StringComparison.Ordinal);
        var stop = end < 0 ? source.Length : end;
        output.Append(source.AsSpan(index, stop - index).IndexOfAny('\n', '\r') >= 0 ? '\n' : ' ');
        return end < 0 ? source.Length : end + 2;
    }
}
