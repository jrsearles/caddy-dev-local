using System.Reflection;
using TextTemplate;

namespace DevLocal.Tool;

internal static class IndexTemplate
{
    private static readonly Lazy<Template> page = new(() => Template.New("index").Parse(CompatibleTemplate()));

    internal static string Css { get; } = ReadAsset("DevLocal.IndexCss");

    internal static string Render<T>(T model) => page.Value.Execute(model);

    private static string CompatibleTemplate()
    {
        var source = ReadAsset("DevLocal.IndexTemplate");
        foreach (var (expression, replacement) in new[]
        {
            ("{{if eq (len .Groups) 1}}", "{{if .OneProject}}"),
            ("{{if eq (len .Rows) 1}}", "{{if .OneService}}"),
            ("{{if and .IsRunning (and (ne .Health \"\") (ne .Health \"none\"))}}", "{{if .ShowHealth}}")
        })
        {
            if (!source.Contains(expression, StringComparison.Ordinal))
                throw new InvalidDataException($"Index template expression changed: {expression}");
            source = source.Replace(expression, replacement, StringComparison.Ordinal);
        }
        return source;
    }

    private static string ReadAsset(string name)
    {
        using var stream = Assembly.GetExecutingAssembly().GetManifestResourceStream(name)
            ?? throw new FileNotFoundException($"Missing embedded asset {name}");
        using var reader = new StreamReader(stream);
        return reader.ReadToEnd();
    }
}
