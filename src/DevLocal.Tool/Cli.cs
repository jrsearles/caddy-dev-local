using Spectre.Console;

namespace DevLocal.Tool;

internal static class Cli
{
    internal static IAnsiConsole Out { get; } = AnsiConsole.Create(
        CreateSettings(System.Console.Out, System.Console.IsOutputRedirected));
    internal static IAnsiConsole Err { get; } = AnsiConsole.Create(
        CreateSettings(System.Console.Error, System.Console.IsErrorRedirected));

    private static AnsiConsoleSettings CreateSettings(TextWriter writer, bool redirected) => new()
    {
        Out = new AnsiConsoleOutput(writer),
        Ansi = redirected ? AnsiSupport.No : AnsiSupport.Detect,
        ColorSystem = redirected ? ColorSystemSupport.NoColors : ColorSystemSupport.Detect
    };
}
