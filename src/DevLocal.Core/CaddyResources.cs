using System.Collections.Immutable;
using System.Text.Json;

namespace DevLocal.Core;

public sealed record CaddyResources(
    ImmutableDictionary<string, JsonElement> Routes,
    ImmutableDictionary<string, JsonElement> Policies);

public static class CaddyResourceGenerator
{
    public static CaddyResources Build(IReadOnlyDictionary<string, ImmutableArray<string>> targets, bool tracing)
    {
        var routes = ImmutableDictionary.CreateBuilder<string, JsonElement>(StringComparer.Ordinal);
        var domains = targets.Keys.OrderBy(domain => domain, StringComparer.Ordinal).ToArray();
        foreach (var domain in domains)
        {
            var id = "devlocal-route-" + domain.Replace(".", "-", StringComparison.Ordinal);
            var upstreams = targets[domain].Select(target => new Dictionary<string, object> { ["dial"] = target }).ToArray();
            var proxy = new Dictionary<string, object>
            {
                ["handler"] = "reverse_proxy",
                ["upstreams"] = upstreams
            };
            var handlers = new List<object>();
            if (tracing)
                handlers.Add(new Dictionary<string, object>
                {
                    ["handler"] = "tracing",
                    ["span"] = "{http.request.method} {http.request.host}"
                });
            handlers.Add(new Dictionary<string, object>
            {
                ["handler"] = "subroute",
                ["routes"] = new object[]
                {
                    new Dictionary<string, object> { ["handle"] = new object[] { proxy } }
                }
            });

            routes[id] = JsonSerializer.SerializeToElement(new Dictionary<string, object>
            {
                ["@id"] = id,
                ["handle"] = handlers,
                ["match"] = new object[] { new Dictionary<string, object> { ["host"] = new[] { domain } } },
                ["terminal"] = true
            });
        }

        var policies = ImmutableDictionary.CreateBuilder<string, JsonElement>(StringComparer.Ordinal);
        if (domains.Length > 0)
            policies["devlocal-tls"] = JsonSerializer.SerializeToElement(new Dictionary<string, object>
            {
                ["@id"] = "devlocal-tls",
                ["issuers"] = new object[] { new Dictionary<string, object> { ["module"] = "internal" } },
                ["subjects"] = domains
            });

        return new CaddyResources(routes.ToImmutable(), policies.ToImmutable());
    }

    public static bool Owns(string id) => id.StartsWith("devlocal-route-", StringComparison.Ordinal) || id == "devlocal-tls";
}
