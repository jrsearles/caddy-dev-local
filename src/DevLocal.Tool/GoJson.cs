using System.Buffers;
using System.Globalization;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.RegularExpressions;

namespace DevLocal.Tool;

internal static class GoJson
{
    private static readonly JsonWriterOptions WriterOptions = new()
    {
        Indented = false,
        Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping
    };

    private static readonly Regex UppercaseEscape =
        new(@"(?<!\\)\\u([0-9A-F]{4})", RegexOptions.Compiled);

    internal static string FromLabels(IReadOnlyDictionary<string, string> labels)
    {
        var buffer = new ArrayBufferWriter<byte>();
        using (var writer = new Utf8JsonWriter(buffer, WriterOptions))
        {
            writer.WriteStartObject();
            foreach (var label in labels.OrderBy(entry => entry.Key, StringComparer.Ordinal))
            {
                writer.WritePropertyName(label.Key);
                writer.WriteStringValue(label.Value);
            }
            writer.WriteEndObject();
        }
        return Finish(Encoding.UTF8.GetString(buffer.WrittenSpan));
    }

    internal static string Canonicalize(string json)
    {
        try
        {
            using var document = JsonDocument.Parse(json);
            var buffer = new ArrayBufferWriter<byte>();
            using (var writer = new Utf8JsonWriter(buffer, WriterOptions))
                WriteElement(writer, document.RootElement);
            return Finish(Encoding.UTF8.GetString(buffer.WrittenSpan));
        }
        catch (JsonException)
        {
            return EscapeRaw(json);
        }
    }

    internal static string EscapeRaw(string json) =>
        json.Replace("&", "\\u0026", StringComparison.Ordinal)
            .Replace("<", "\\u003c", StringComparison.Ordinal)
            .Replace(">", "\\u003e", StringComparison.Ordinal);

    private static string Finish(string json) =>
        UppercaseEscape.Replace(EscapeRaw(json), match => "\\u" + match.Groups[1].Value.ToLowerInvariant());

    private static void WriteElement(Utf8JsonWriter writer, JsonElement element)
    {
        switch (element.ValueKind)
        {
            case JsonValueKind.Object:
                writer.WriteStartObject();
                foreach (var property in element.EnumerateObject().OrderBy(entry => entry.Name, StringComparer.Ordinal))
                {
                    writer.WritePropertyName(property.Name);
                    WriteElement(writer, property.Value);
                }
                writer.WriteEndObject();
                break;
            case JsonValueKind.Array:
                writer.WriteStartArray();
                foreach (var item in element.EnumerateArray())
                    WriteElement(writer, item);
                writer.WriteEndArray();
                break;
            case JsonValueKind.String:
                writer.WriteStringValue(element.GetString());
                break;
            case JsonValueKind.Number:
                WriteNumber(writer, element.GetRawText());
                break;
            case JsonValueKind.True:
                writer.WriteBooleanValue(true);
                break;
            case JsonValueKind.False:
                writer.WriteBooleanValue(false);
                break;
            default:
                writer.WriteNullValue();
                break;
        }
    }

    private static void WriteNumber(Utf8JsonWriter writer, string raw)
    {
        if (long.TryParse(raw, NumberStyles.Integer, CultureInfo.InvariantCulture, out var integer))
        {
            writer.WriteNumberValue(integer);
            return;
        }
        if (double.TryParse(raw, NumberStyles.Float, CultureInfo.InvariantCulture, out var number) &&
            number != 0 && (Math.Abs(number) < 1e-6 || Math.Abs(number) >= 1e21))
        {
            writer.WriteRawValue(number.ToString("R", CultureInfo.InvariantCulture)
                .Replace("E", "e", StringComparison.Ordinal));
            return;
        }
        if (decimal.TryParse(raw, NumberStyles.Float, CultureInfo.InvariantCulture, out var exact))
        {
            writer.WriteRawValue(exact.ToString(CultureInfo.InvariantCulture));
            return;
        }
        writer.WriteRawValue(raw);
    }
}
