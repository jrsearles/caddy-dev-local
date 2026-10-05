namespace DevLocal.Ui;

public sealed class UiSession
{
    public string Filter { get; private set; } = "";

    public event Action? FilterChanged;

    public ContainerModel? Selected { get; private set; } = null;

    public event Action? SelectionChanged;

    public void SetFilter(string value)
    {
        if (value == Filter)
            return;
        Filter = value;
        FilterChanged?.Invoke();
    }

    public void Select(ContainerModel? container)
    {
        Selected = container;
        SelectionChanged?.Invoke();
    }
}