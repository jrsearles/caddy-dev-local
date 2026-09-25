using System.Collections.Immutable;
using System.Net;
using System.Text.Json;
using System.Text.Json.Nodes;
using DevLocal.Core;
using DevLocal.Tool;
using Xunit;

namespace DevLocal.Tool.Tests;

public sealed class CaddyApiClientTests
{
    [Fact]
    public async Task AdoptsExistingRoutesAndPoliciesWithoutTouchingUserConfiguration()
    {
        var state = new FakeCaddy { ServerExists = true, PoliciesExist = true };
        state.Routes.Add(JsonNode.Parse("""{"@id":"user-route"}""")!);
        state.Routes.Add(JsonNode.Parse("""{"@id":"devlocal-route-web-dev-local","value":"old"}""")!);
        state.Routes.Add(JsonNode.Parse("""{"@id":"devlocal-route-orphan"}""")!);
        state.Policies.Add(JsonNode.Parse("""{"@id":"user-policy"}""")!);
        state.Policies.Add(JsonNode.Parse("""{"@id":"devlocal-tls","subjects":["old"]}""")!);
        using var http = new HttpClient(state);
        using var client = new CaddyApiClient("http://localhost:2019", client: http);
        var desired = CaddyResourceGenerator.Build(new Dictionary<string, ImmutableArray<string>>
        {
            ["web.dev.local"] = ["localhost:8080"],
            ["api.dev.local"] = ["localhost:8181"]
        }, tracing: false);

        await client.ReconcileAsync(desired, CaddyResourceGenerator.Owns, CancellationToken.None);

        Assert.Contains("PATCH /id/devlocal-route-web-dev-local", state.Writes);
        Assert.Contains("POST /config/apps/http/servers/srv0/routes/-", state.Writes);
        Assert.Contains("DELETE /id/devlocal-route-orphan", state.Writes);
        Assert.Contains(state.Routes, route => route["@id"]!.GetValue<string>() == "user-route");
        Assert.Equal(new[] { "devlocal-tls", "user-policy" }, state.Policies.Select(item => item["@id"]!.GetValue<string>()));

        state.Writes.Clear();
        await client.ReconcileAsync(desired, CaddyResourceGenerator.Owns, CancellationToken.None);
        Assert.Empty(state.Writes);
    }

    [Fact]
    public async Task CreatesMissingServerUsingCaddyEffectivePorts()
    {
        var state = new FakeCaddy { HttpPort = 9080, HttpsPort = 9443 };
        using var http = new HttpClient(state);
        using var client = new CaddyApiClient("http://localhost:2019", client: http);
        var desired = CaddyResourceGenerator.Build(new Dictionary<string, ImmutableArray<string>>
        {
            ["web.dev.local"] = ["localhost:8080"]
        }, tracing: true);

        await client.ReconcileAsync(desired, CaddyResourceGenerator.Owns, CancellationToken.None);

        Assert.Equal(new[] { ":9443", ":9080" }, state.Listen);
        Assert.Contains(state.Routes, item => item["@id"]!.GetValue<string>() == "devlocal-route-web-dev-local");
        Assert.Contains("PUT /config/apps/http/servers/srv0", state.Writes);
    }

    [Fact]
    public async Task PreservesOtherHookPoliciesAndCleansOnlyOwnedResources()
    {
        var state = new FakeCaddy { ServerExists = true, PoliciesExist = true };
        state.Routes.Add(JsonNode.Parse("""{"@id":"devlocal-index"}""")!);
        state.Routes.Add(JsonNode.Parse("""{"@id":"user-route"}""")!);
        state.Policies.Add(JsonNode.Parse("""{"@id":"devlocal-tls-ui","subjects":["dev.local"]}""")!);
        state.Policies.Add(JsonNode.Parse("""{"@id":"user-policy"}""")!);
        using var http = new HttpClient(state);
        using var client = new CaddyApiClient("http://localhost:2019", client: http);

        await client.ReconcileAsync(CaddyResourceGenerator.Build(new Dictionary<string, ImmutableArray<string>>
        {
            ["web.dev.local"] = ["localhost:8080"]
        }, tracing: false), CaddyResourceGenerator.Owns, CancellationToken.None);

        Assert.Equal(new[] { "devlocal-tls", "devlocal-tls-ui", "user-policy" },
            state.Policies.Select(item => item["@id"]!.GetValue<string>()));
        Assert.Contains(state.Routes, item => item["@id"]!.GetValue<string>() == "devlocal-index");

        await client.CleanupAsync(CancellationToken.None);

        Assert.Single(state.Routes);
        Assert.Equal("user-route", state.Routes[0]["@id"]!.GetValue<string>());
        Assert.Single(state.Policies);
        Assert.Equal("user-policy", state.Policies[0]["@id"]!.GetValue<string>());
    }

    [Fact]
    public async Task CleanupDoesNotCreateMissingCaddyServer()
    {
        var state = new FakeCaddy();
        using var http = new HttpClient(state);
        using var client = new CaddyApiClient("http://localhost:2019", client: http);

        await client.CleanupAsync(CancellationToken.None);

        Assert.False(state.ServerExists);
        Assert.Empty(state.Writes);
    }

    [Fact]
    public async Task ReconcileHonorsCreationPolicyWhenServerIsMissing()
    {
        var state = new FakeCaddy();
        using var http = new HttpClient(state);
        using var client = new CaddyApiClient("http://localhost:2019", allowCreateServer: false, client: http);

        var exception = await Assert.ThrowsAsync<InvalidOperationException>(() =>
            client.ReconcileAsync(CaddyResourceGenerator.Build(new Dictionary<string, ImmutableArray<string>>(), false),
                CaddyResourceGenerator.Owns, CancellationToken.None));

        Assert.Contains("creation is disabled", exception.Message);
        Assert.False(state.ServerExists);
        Assert.Empty(state.Writes);
    }

    [Fact]
    public async Task PatchNotFoundReaddsRouteAfterConcurrentRemoval()
    {
        var state = new FakeCaddy { ServerExists = true, PoliciesExist = true, DropOnPatch = true };
        state.Routes.Add(JsonNode.Parse("""{"@id":"devlocal-route-web-dev-local","obsolete":true}""")!);
        using var http = new HttpClient(state);
        using var client = new CaddyApiClient("http://localhost:2019", client: http);
        var desired = CaddyResourceGenerator.Build(new Dictionary<string, ImmutableArray<string>>
        {
            ["web.dev.local"] = ["localhost:8080"]
        }, tracing: false);

        await client.ReconcileAsync(desired, CaddyResourceGenerator.Owns, CancellationToken.None);

        Assert.Contains("PATCH /id/devlocal-route-web-dev-local", state.Writes);
        Assert.Contains("POST /config/apps/http/servers/srv0/routes/-", state.Writes);
        Assert.Single(state.Routes);
        Assert.Null(state.Routes[0]["obsolete"]);
    }

    private sealed class FakeCaddy : HttpMessageHandler
    {
        public bool ServerExists { get; set; }
        public bool PoliciesExist { get; set; }
        public bool DropOnPatch { get; set; }
        public int HttpPort { get; set; } = 80;
        public int HttpsPort { get; set; } = 443;
        public string[] Listen { get; private set; } = [];
        public List<JsonNode> Routes { get; } = [];
        public List<JsonNode> Policies { get; private set; } = [];
        public List<string> Writes { get; } = [];

        protected override async Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
        {
            var path = request.RequestUri!.AbsolutePath;
            var method = request.Method.Method;
            if (request.Method != HttpMethod.Get)
                Writes.Add($"{method} {path}");
            var body = request.Content is null ? null : JsonNode.Parse(await request.Content.ReadAsStringAsync(cancellationToken));

            if (method == "GET" && path == "/config/")
                return Respond(HttpStatusCode.OK, """{"apps":{}}""");
            if (method == "GET" && path == "/config/apps/http")
                return Respond(HttpStatusCode.OK, JsonSerializer.Serialize(new { http_port = HttpPort, https_port = HttpsPort }));
            if (path == "/config/apps/http/servers/srv0")
            {
                if (method == "GET")
                    return ServerExists ? Respond(HttpStatusCode.OK, "{}") : Respond(HttpStatusCode.NotFound, "server missing");
                if (method == "PUT")
                {
                    ServerExists = true;
                    Listen = body!["listen"]!.AsArray().Select(item => item!.GetValue<string>()).ToArray();
                    return Respond(HttpStatusCode.OK, "{}");
                }
            }
            if (path == "/config/apps/http/servers/srv0/routes")
            {
                if (method == "GET")
                    return ServerExists ? Respond(HttpStatusCode.OK, JsonSerializer.Serialize(Routes))
                        : Respond(HttpStatusCode.BadRequest, "invalid traversal path");
                if (method == "PUT")
                    return Respond(HttpStatusCode.OK, "{}");
            }
            if (path == "/config/apps/http/servers/srv0/routes/-" && method == "POST")
            {
                Routes.Add(body!);
                return Respond(HttpStatusCode.OK, "{}");
            }
            if (path.StartsWith("/id/", StringComparison.Ordinal))
            {
                var id = Uri.UnescapeDataString(path[4..]);
                var index = Routes.FindIndex(route => route["@id"]?.GetValue<string>() == id);
                if (index == -1)
                    return Respond(HttpStatusCode.NotFound, "missing id");
                if (method == "PATCH" && DropOnPatch)
                {
                    DropOnPatch = false;
                    Routes.RemoveAt(index);
                    return Respond(HttpStatusCode.NotFound, "missing id");
                }
                if (method == "PATCH")
                    Routes[index] = body!;
                if (method == "DELETE")
                    Routes.RemoveAt(index);
                return Respond(HttpStatusCode.OK, "{}");
            }
            if (path == "/config/apps/tls/automation/policies")
            {
                if (method == "GET")
                    return PoliciesExist ? Respond(HttpStatusCode.OK, JsonSerializer.Serialize(Policies))
                        : Respond(HttpStatusCode.NotFound, "missing policies");
                if (method is "PUT" or "PATCH")
                {
                    PoliciesExist = true;
                    Policies = body!.AsArray().Select(item => item!.DeepClone()).ToList();
                    return Respond(HttpStatusCode.OK, "{}");
                }
            }
            return Respond(HttpStatusCode.NotFound, "unexpected request");
        }

        private static HttpResponseMessage Respond(HttpStatusCode status, string body) => new(status)
        {
            Content = new StringContent(body)
        };
    }
}
