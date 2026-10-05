using System.Reflection;

namespace DevLocal.Ui;

public static class UiAssets
{
    private static readonly Lazy<string> css = new(() => Read("DevLocal.IndexCss"));

    private static readonly Lazy<string> js = new(() => Read("DevLocal.UiJs"));

    public static string Css => css.Value;

    public static string Js => js.Value;

    private static string Read(string name)
    {
        using var stream = Assembly.GetExecutingAssembly().GetManifestResourceStream(name)
            ?? throw new FileNotFoundException($"Missing embedded asset {name}");
        using var reader = new StreamReader(stream);
        return reader.ReadToEnd().Replace("\r\n", "\n", StringComparison.Ordinal);
    }
}