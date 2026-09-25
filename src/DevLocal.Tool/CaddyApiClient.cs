using System.Net;
using System.Net.Http.Headers;
using System.Text;
using System.Text.Json;
using DevLocal.Core;

namespace DevLocal.Tool;

internal interface ICaddyConfigClient
{
    Task ReconcileAsync(CaddyResources desired, Predicate<string> owned, CancellationToken token);
    Task CleanupAsync(CancellationToken token);
    Task<string> RunningConfigAsync(CancellationToken token);
}

internal sealed class CaddyApiClient : ICaddyConfigClient, IDisposable
{
    private readonly string baseUrl;
    private readonly string serverName;
    private readonly bool allowCreateServer;
    private readonly HttpClient http;
    private readonly bool ownsHttp;
    private readonly SemaphoreSlim reconcileGate = new(1, 1);

    internal CaddyApiClient(string baseUrl, string serverName = "srv0", bool allowCreateServer = true, HttpClient? client = null)
    {
        this.baseUrl = baseUrl.TrimEnd('/');
        this.serverName = serverName;
        this.allowCreateServer = allowCreateServer;
        ownsHttp = client is null;
        http = client ?? new HttpClient(new SocketsHttpHandler { PooledConnectionLifetime = TimeSpan.Zero })
        {
            Timeout = TimeSpan.FromSeconds(30)
        };
    }

    public async Task<string> RunningConfigAsync(CancellationToken token)
    {
        var (status, body) = await RequestAsync(HttpMethod.Get, "/config/", null, token);
        EnsureSuccess(HttpMethod.Get, "/config/", status, body);
        return Encoding.UTF8.GetString(body);
    }

    public async Task ReconcileAsync(CaddyResources desired, Predicate<string> owned, CancellationToken token)
    {
        await reconcileGate.WaitAsync(token);
        try
        {
            var routesPath = $"/config/apps/http/servers/{Uri.EscapeDataString(serverName)}/routes";
            var actualRoutes = await GetArrayAsync(routesPath, token);
            if (actualRoutes is null)
            {
                if (!allowCreateServer)
                    throw new InvalidOperationException($"Target Caddy HTTP server {serverName} is absent and server creation is disabled");
                await EnsureRoutesAsync(routesPath, token);
                actualRoutes = await GetArrayAsync(routesPath, token)
                    ?? throw new InvalidOperationException("Caddy routes are still missing after creating the server");
            }

            await ReconcileRoutesAsync(routesPath, actualRoutes, desired.Routes, owned, token);
            await ReconcilePoliciesAsync(desired.Policies, owned, token);
        }
        finally
        {
            reconcileGate.Release();
        }
    }

    public async Task CleanupAsync(CancellationToken token)
    {
        await reconcileGate.WaitAsync(token);
        try
        {
            var errors = new List<Exception>();
            var routesPath = $"/config/apps/http/servers/{Uri.EscapeDataString(serverName)}/routes";
            try
            {
                var routes = await GetArrayAsync(routesPath, token);
                if (routes is not null)
                    await ReconcileRoutesAsync(routesPath, routes, new Dictionary<string, JsonElement>(), OwnsAny, token);
            }
            catch (Exception ex) when (!token.IsCancellationRequested)
            {
                errors.Add(ex);
            }

            try
            {
                const string path = "/config/apps/tls/automation/policies";
                var policies = await GetArrayAsync(path, token);
                if (policies is not null)
                {
                    var remaining = policies.Where(policy => !OwnsAny(ResourceId(policy))).ToArray();
                    if (!ArraysEqual(policies, remaining))
                        await ChangeAsync(HttpMethod.Patch, path, JsonSerializer.SerializeToUtf8Bytes(remaining), token);
                }
            }
            catch (Exception ex) when (!token.IsCancellationRequested)
            {
                errors.Add(ex);
            }

            if (errors.Count > 0)
                throw new AggregateException(errors);
        }
        finally
        {
            reconcileGate.Release();
        }
    }

    private async Task EnsureRoutesAsync(string routesPath, CancellationToken token)
    {
        var serverPath = $"/config/apps/http/servers/{Uri.EscapeDataString(serverName)}";
        var (status, body) = await RequestAsync(HttpMethod.Get, serverPath, null, token);
        if (IsMissing(status, body))
        {
            await CreateServerAsync(serverPath, token);
            return;
        }
        EnsureSuccess(HttpMethod.Get, serverPath, status, body);
        (status, body) = await RequestAsync(HttpMethod.Put, routesPath, "[]"u8.ToArray(), token);
        if (status != HttpStatusCode.Conflict)
            EnsureSuccess(HttpMethod.Put, routesPath, status, body);
    }

    private async Task CreateServerAsync(string serverPath, CancellationToken token)
    {
        var (status, body) = await RequestAsync(HttpMethod.Get, "/config/apps/http", null, token);
        var httpPort = 80;
        var httpsPort = 443;
        if (!IsMissing(status, body))
        {
            EnsureSuccess(HttpMethod.Get, "/config/apps/http", status, body);
            using var config = JsonDocument.Parse(body);
            if (config.RootElement.TryGetProperty("http_port", out var httpValue) && httpValue.GetInt32() != 0)
                httpPort = httpValue.GetInt32();
            if (config.RootElement.TryGetProperty("https_port", out var httpsValue) && httpsValue.GetInt32() != 0)
                httpsPort = httpsValue.GetInt32();
        }

        var server = new Dictionary<string, object>
        {
            ["listen"] = new[] { $":{httpsPort}", $":{httpPort}" },
            ["routes"] = Array.Empty<object>()
        };
        (status, body) = await RequestAsync(HttpMethod.Put, serverPath, JsonSerializer.SerializeToUtf8Bytes(server), token);
        if (status != HttpStatusCode.Conflict)
            EnsureSuccess(HttpMethod.Put, serverPath, status, body);
    }

    private async Task ReconcileRoutesAsync(
        string routesPath,
        JsonElement[] actual,
        IReadOnlyDictionary<string, JsonElement> desired,
        Predicate<string> owned,
        CancellationToken token)
    {
        var actualOwned = new Dictionary<string, JsonElement>(StringComparer.Ordinal);
        foreach (var route in actual)
        {
            var id = ResourceId(route);
            if (owned(id))
                actualOwned[id] = route;
        }

        foreach (var id in desired.Keys.Order(StringComparer.Ordinal))
        {
            var route = desired[id];
            var payload = Encoding.UTF8.GetBytes(route.GetRawText());
            if (actualOwned.TryGetValue(id, out var existing))
            {
                if (JsonElement.DeepEquals(existing, route))
                    continue;
                var idPath = "/id/" + Uri.EscapeDataString(id);
                var (status, body) = await RequestAsync(HttpMethod.Patch, idPath, payload, token);
                if (status != HttpStatusCode.NotFound)
                {
                    EnsureSuccess(HttpMethod.Patch, idPath, status, body);
                    continue;
                }
            }
            await ChangeAsync(HttpMethod.Post, routesPath + "/-", payload, token);
        }

        foreach (var id in actualOwned.Keys.Order(StringComparer.Ordinal))
        {
            if (desired.ContainsKey(id))
                continue;
            var path = "/id/" + Uri.EscapeDataString(id);
            var (status, body) = await RequestAsync(HttpMethod.Delete, path, null, token);
            if (status != HttpStatusCode.NotFound)
                EnsureSuccess(HttpMethod.Delete, path, status, body);
        }
    }

    private async Task ReconcilePoliciesAsync(
        IReadOnlyDictionary<string, JsonElement> desired,
        Predicate<string> owned,
        CancellationToken token)
    {
        const string path = "/config/apps/tls/automation/policies";
        var actual = await GetArrayAsync(path, token);
        var managed = new Dictionary<string, JsonElement>(desired, StringComparer.Ordinal);
        var userPolicies = new List<JsonElement>();
        foreach (var policy in actual ?? [])
        {
            var id = ResourceId(policy);
            if (id == "")
            {
                userPolicies.Add(policy);
                continue;
            }
            if (owned(id) && !desired.ContainsKey(id))
                continue;
            if (OwnsAny(id))
                managed.TryAdd(id, policy);
            else
                userPolicies.Add(policy);
        }

        var merged = managed.OrderBy(entry => entry.Key, StringComparer.Ordinal)
            .Select(entry => entry.Value).Concat(userPolicies).ToArray();
        if (actual is not null && ArraysEqual(actual, merged))
            return;
        await ChangeAsync(actual is null ? HttpMethod.Put : HttpMethod.Patch,
            path, JsonSerializer.SerializeToUtf8Bytes(merged), token);
    }

    private async Task<JsonElement[]?> GetArrayAsync(string path, CancellationToken token)
    {
        var (status, body) = await RequestAsync(HttpMethod.Get, path, null, token);
        if (IsMissing(status, body))
            return null;
        EnsureSuccess(HttpMethod.Get, path, status, body);
        if (body.Length == 0 || Encoding.UTF8.GetString(body) == "null")
            return [];
        using var document = JsonDocument.Parse(body);
        return document.RootElement.EnumerateArray().Select(item => item.Clone()).ToArray();
    }

    private async Task ChangeAsync(HttpMethod method, string path, byte[] payload, CancellationToken token)
    {
        var (status, body) = await RequestAsync(method, path, payload, token);
        EnsureSuccess(method, path, status, body);
    }

    private async Task<(HttpStatusCode Status, byte[] Body)> RequestAsync(
        HttpMethod method, string path, byte[]? payload, CancellationToken token)
    {
        using var request = new HttpRequestMessage(method, baseUrl + path);
        request.Headers.ConnectionClose = true;
        if (payload is not null)
        {
            request.Content = new ByteArrayContent(payload);
            request.Content.Headers.ContentType = new MediaTypeHeaderValue("application/json");
        }
        using var response = await http.SendAsync(request, token);
        return (response.StatusCode, await response.Content.ReadAsByteArrayAsync(token));
    }

    private static string ResourceId(JsonElement resource) =>
        resource.ValueKind == JsonValueKind.Object &&
        resource.TryGetProperty("@id", out var value) && value.ValueKind == JsonValueKind.String
            ? value.GetString() ?? ""
            : "";

    private static bool OwnsAny(string id) => id.StartsWith("devlocal-", StringComparison.Ordinal);

    private static bool ArraysEqual(JsonElement[] left, JsonElement[] right) =>
        left.Length == right.Length && left.Zip(right).All(pair => JsonElement.DeepEquals(pair.First, pair.Second));

    private static bool IsMissing(HttpStatusCode status, byte[] body) =>
        status == HttpStatusCode.NotFound ||
        status == HttpStatusCode.BadRequest && Encoding.UTF8.GetString(body).Contains("invalid traversal path", StringComparison.Ordinal);

    private static void EnsureSuccess(HttpMethod method, string path, HttpStatusCode status, byte[] body)
    {
        if ((int)status >= 200 && (int)status < 300)
            return;
        var text = Encoding.UTF8.GetString(body).Trim();
        if (text == "")
            text = status.ToString();
        throw new HttpRequestException($"{method} {path} returned {(int)status}: {text}");
    }

    public void Dispose()
    {
        if (ownsHttp)
            http.Dispose();
        reconcileGate.Dispose();
    }
}
