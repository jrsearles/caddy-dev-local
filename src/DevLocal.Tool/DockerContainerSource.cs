using System.Collections.Immutable;
using DevLocal.Core;
using Docker.DotNet;

namespace DevLocal.Tool;

internal sealed class DockerContainerSource(IDockerClient client) : IContainerSnapshotSource
{
    public Task<ImmutableArray<ContainerInfo>> ListAsync(CancellationToken token) =>
        DockerDiscoveryClient.ListAsync(client, token);
}
