using System.Text.Json;

namespace DevLocal.Ui;

public static class CaddyConfig
{
    public static List<RouteModel> ParseRoutes(string configJson)
    {
        var routes = new List<RouteModel>();
        if (configJson.Length == 0)
            return routes;
        try
        {
            using var document = JsonDocument.Parse(configJson);
            if (document.RootElement.ValueKind != JsonValueKind.Object)
                return routes;
            var root = document.RootElement;
            var apps = Property(root, "apps");
            var http = Property(apps, "http");
            var servers = Property(http, "servers");
            var issuers = Issuers(apps);

            foreach (var server in Members(servers))
            {
                foreach (var route in Items(Property(server.Value, "routes")))
                {
                    var id = Text(Property(route, "@id"));
                    var hosts = new List<string>();
                    var paths = new List<string>();
                    foreach (var match in Items(Property(route, "match")))
                    {
                        foreach (var host in Strings(Property(match, "host")))
                            hosts.Add(host);
                        foreach (var path in Strings(Property(match, "path")))
                            paths.Add(path);
                    }

                    var handlers = new List<string>();
                    var upstreams = new List<string>();
                    var root_ = "";
                    Collect(Property(route, "handle"), handlers, upstreams, ref root_);

                    var (tls, tlsClass) = Certificate(hosts, issuers);
                    routes.Add(new RouteModel(id, hosts, paths, handlers, upstreams, root_, tls, tlsClass));
                }
            }
        }
        catch (JsonException) { }
        return routes;
    }

    private static void Collect(JsonElement handlers, List<string> names, List<string> upstreams, ref string root)
    {
        foreach (var handler in Items(handlers))
        {
            var name = Text(Property(handler, "handler"));
            if (name == "subroute")
            {
                foreach (var subroute in Items(Property(handler, "routes")))
                    Collect(Property(subroute, "handle"), names, upstreams, ref root);
                continue;
            }
            if (name.Length > 0)
                names.Add(name);
            foreach (var upstream in Items(Property(handler, "upstreams")))
            {
                var dial = Text(Property(upstream, "dial"));
                if (dial.Length > 0)
                    upstreams.Add(dial);
            }
            if (root.Length == 0)
                root = Text(Property(handler, "root"));
        }
    }

    private static Dictionary<string, string> Issuers(JsonElement apps)
    {
        var issuers = new Dictionary<string, string>(StringComparer.Ordinal);
        var policies = Items(Property(Property(Property(apps, "tls"), "automation"), "policies"));
        foreach (var policy in policies)
        {
            var modules = new List<string>();
            foreach (var issuer in Items(Property(policy, "issuers")))
                modules.Add(Text(Property(issuer, "module")));
            var first = modules.Count > 0 ? modules[0] : "none";
            foreach (var subject in Items(Property(policy, "subjects")))
            {
                var domain = subject.ValueKind == JsonValueKind.String
                    ? Text(subject)
                    : Text(Property(subject, "subject"));
                if (domain.Length > 0)
                    issuers[domain] = first;
            }
        }
        return issuers;
    }

    private static (string Label, string Class) Certificate(List<string> hosts, Dictionary<string, string> issuers)
    {
        foreach (var host in hosts)
        {
            if (!issuers.TryGetValue(host, out var module))
                continue;
            return module is "internal" or "caddy" || module == "none"
                ? (module == "none" ? "none" : "internal CA", "tls-internal")
                : (module, "tls-external");
        }
        return ("", "");
    }

    private static JsonElement Property(JsonElement element, string name) =>
        element.ValueKind == JsonValueKind.Object && element.TryGetProperty(name, out var value) ? value : default;

    private static IEnumerable<JsonElement> Items(JsonElement element) =>
        element.ValueKind == JsonValueKind.Array ? element.EnumerateArray() : [];

    private static IEnumerable<JsonProperty> Members(JsonElement element) =>
        element.ValueKind == JsonValueKind.Object ? element.EnumerateObject() : [];

    private static string Text(JsonElement element) =>
        element.ValueKind == JsonValueKind.String ? element.GetString() ?? "" : "";

    private static IEnumerable<string> Strings(JsonElement element)
    {
        if (element.ValueKind != JsonValueKind.Array)
            yield break;
        foreach (var item in element.EnumerateArray())
        {
            if (item.ValueKind == JsonValueKind.String && item.GetString() is { Length: > 0 } value)
                yield return value;
        }
    }
}