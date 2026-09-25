using System.Net.Http;
using DevLocal.Core;

namespace DevLocal.Tool;

internal sealed class HttpPortProbe : IPortProbe
{
    internal static IReadOnlyList<ushort> PreferredPorts(IReadOnlyList<ushort> ports)
    {
        if (ports.Count <= 1)
            return ports.ToArray();

        var preferred = new ushort[] { 80, 8080, 443, 8443 };
        return preferred.Where(ports.Contains)
            .Concat(ports.Where(port => !preferred.Contains(port)))
            .ToArray();
    }

    public async Task<ushort> ProbeAsync(string host, IReadOnlyList<ushort> ports, TimeSpan timeout, CancellationToken token)
    {
        if (ports.Count == 0)
            throw new ArgumentException("No ports to probe", nameof(ports));

        using var handler = new SocketsHttpHandler
        {
            AllowAutoRedirect = false,
            ConnectTimeout = timeout,
            PooledConnectionLifetime = TimeSpan.Zero,
            UseProxy = false
        };
        using var client = new HttpClient(handler) { Timeout = timeout };
        Exception? lastError = null;
        foreach (var port in PreferredPorts(ports))
        {
            foreach (var address in host == "localhost" ? new[] { host, "127.0.0.1" } : [host])
            {
                try
                {
                    using var request = new HttpRequestMessage(HttpMethod.Get, $"http://{address}:{port}/");
                    request.Headers.Host = $"{host}:{port}";
                    request.Headers.UserAgent.ParseAdd("DevLocal-Server-Detection");
                    using var response = await client.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, token);
                    return port;
                }
                catch (OperationCanceledException) when (token.IsCancellationRequested)
                {
                    throw;
                }
                catch (Exception error)
                {
                    lastError = error;
                }
            }
        }

        throw new HttpRequestException("No HTTP port found: " + lastError?.Message, lastError);
    }
}
