using Microsoft.JSInterop;

namespace DevLocal.Ui;

public sealed class UiInterop(IJSRuntime runtime)
{
    public ValueTask<string> CycleThemeAsync() => runtime.InvokeAsync<string>("devlocalUi.cycleTheme");

    public ValueTask<string> ThemeAsync() => runtime.InvokeAsync<string>("devlocalUi.theme");

    public ValueTask CopyAsync(string text) => runtime.InvokeVoidAsync("devlocalUi.copy", text);

    public ValueTask ScrollToAsync(string elementId) => runtime.InvokeVoidAsync("devlocalUi.scrollTo", elementId);

    public ValueTask FocusSearchAsync() => runtime.InvokeVoidAsync("devlocalUi.focusSearch");
}

public sealed class Clock : IDisposable
{
    private readonly Timer timer;

    public Clock() => timer = new(_ => OnTick(), null, TimeSpan.FromSeconds(10), TimeSpan.FromSeconds(10));

    public event Action? Ticked;

    public static string Relative(DateTimeOffset value)
    {
        var seconds = Math.Max(0, (int)(DateTimeOffset.UtcNow - value).TotalSeconds);
        if (seconds < 10) return "just now";
        if (seconds < 60) return seconds + "s ago";
        var minutes = seconds / 60;
        if (minutes < 60) return minutes + "m ago";
        var hours = minutes / 60;
        if (hours < 24) return hours + "h ago";
        var days = hours / 24;
        if (days < 30) return days + "d ago";
        var months = days / 30.44;
        if (months < 12) return Math.Round(months) + "mo ago";
        return Math.Round(months / 12) + "y ago";
    }

    public string Label(DateTimeOffset? value) =>
        value is { } moment ? Relative(moment) : "";

    private void OnTick() => Ticked?.Invoke();

    public void Dispose() => timer.Dispose();
}