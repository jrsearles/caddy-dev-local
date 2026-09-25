using System.Net;
using System.Net.Sockets;
using System.Text;
using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class HttpPortProbeTests
{
    [Fact]
    public void PrefersWellKnownPublishedHostPortsInGoOrder() =>
        Assert.Equal(new ushort[] { 80, 8080, 443, 8443, 32080, 32081 },
            HttpPortProbe.PreferredPorts([32080, 8443, 443, 32081, 80, 8080]));

    [Fact]
    public async Task AcceptsAnyHttpStatusWithoutFollowingRedirects()
    {
        using var listener = new TcpListener(IPAddress.Loopback, 0);
        listener.Start();
        var port = (ushort)((IPEndPoint)listener.LocalEndpoint).Port;
        var respond = Task.Run(async () =>
        {
            using var connection = await listener.AcceptTcpClientAsync();
            await using var stream = connection.GetStream();
            var buffer = new byte[2048];
            var read = await stream.ReadAsync(buffer);
            Assert.Contains("DevLocal-Server-Detection", Encoding.ASCII.GetString(buffer, 0, read));
            await stream.WriteAsync("HTTP/1.1 302 Found\r\nLocation: http://nonexistent.invalid/\r\nContent-Length: 0\r\n\r\n"u8.ToArray());
        });

        var result = await new HttpPortProbe().ProbeAsync("localhost", [port], TimeSpan.FromSeconds(2), CancellationToken.None);

        Assert.Equal(port, result);
        await respond.WaitAsync(TimeSpan.FromSeconds(2));
    }
}
