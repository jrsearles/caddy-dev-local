namespace DevLocal.Tool;

internal static class ServicePaths
{
    internal const string Name = "DevLocal";
    internal static string ProgramDirectory => Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.ProgramFiles), Name);
    internal static string DataDirectory => Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.CommonApplicationData), Name);
    internal static string ConfigPath => Path.Combine(DataDirectory, "service.json");
    internal static string StatusPath => Path.Combine(DataDirectory, "status.json");
    internal static string LogPath => Path.Combine(DataDirectory, "service.log");
}
