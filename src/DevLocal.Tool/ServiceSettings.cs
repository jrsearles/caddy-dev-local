using System.Text.Json;

namespace DevLocal.Tool;

internal sealed record ServiceSettings(string CaddyAdmin, string DockerPipe)
{
    public AppOptions? Options { get; init; }
    public bool ControllerEnabled { get; init; }

    internal static ServiceSettings Default => new("http://localhost:2019", "docker_engine");

    internal static async Task<ServiceSettings> LoadAsync(CancellationToken token)
    {
        await using var stream = File.OpenRead(ServicePaths.ConfigPath);
        return await JsonSerializer.DeserializeAsync<ServiceSettings>(stream, cancellationToken: token)
            ?? throw new InvalidDataException("Service configuration is empty");
    }

    internal async Task SaveAsync(CancellationToken token)
    {
        Directory.CreateDirectory(ServicePaths.DataDirectory);
        await using var stream = File.Create(ServicePaths.ConfigPath);
        await JsonSerializer.SerializeAsync(stream, this, cancellationToken: token);
    }
}
