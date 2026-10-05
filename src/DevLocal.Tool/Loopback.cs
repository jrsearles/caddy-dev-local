namespace DevLocal.Tool;

internal static class Loopback
{
    internal static string NormalizeUrl(string url)
    {
        var trimmed = url.TrimEnd('/');
        if (Uri.TryCreate(trimmed, UriKind.Absolute, out var uri)
            && uri.Scheme == "http"
            && uri.Host.Equals("localhost", StringComparison.OrdinalIgnoreCase))
        {
            return new UriBuilder(uri) { Host = "127.0.0.1" }.Uri.AbsoluteUri.TrimEnd('/');
        }

        return trimmed;
    }
}