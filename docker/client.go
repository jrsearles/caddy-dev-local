package docker

import (
	"context"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type Client interface {
	ContainerList(ctx context.Context, options client.ContainerListOptions) ([]container.Summary, error)
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
	Events(ctx context.Context, options client.EventsListOptions) (<-chan events.Message, <-chan error)
	NetworkInspect(ctx context.Context, networkID string) (network.Inspect, error)
}

type DockerClient struct {
	api *client.Client
}

func NewClient() (*DockerClient, error) {
	apiClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation()) //nolint:govet,staticcheck // inline false positive from external package; deprecated but required
	if err != nil {
		return nil, err
	}
	return &DockerClient{api: apiClient}, nil
}

func (c *DockerClient) ContainerList(ctx context.Context, options client.ContainerListOptions) ([]container.Summary, error) {
	result, err := c.api.ContainerList(ctx, options)
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (c *DockerClient) ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error) {
	result, err := c.api.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return container.InspectResponse{}, err
	}
	return result.Container, nil
}

func (c *DockerClient) Events(ctx context.Context, options client.EventsListOptions) (<-chan events.Message, <-chan error) {
	result := c.api.Events(ctx, options)
	return result.Messages, result.Err
}

func (c *DockerClient) NetworkInspect(ctx context.Context, networkID string) (network.Inspect, error) {
	result, err := c.api.NetworkInspect(ctx, networkID, client.NetworkInspectOptions{})
	if err != nil {
		return network.Inspect{}, err
	}
	return result.Network, nil
}

func LabelValue(c *container.Summary, key string) string {
	if c.Labels == nil {
		return ""
	}
	return c.Labels[key]
}

func ComposeProject(c *container.Summary) string {
	return LabelValue(c, "com.docker.compose.project")
}

func ComposeService(c *container.Summary) string {
	return LabelValue(c, "com.docker.compose.service")
}

func ContainerName(c *container.Summary) string {
	for _, name := range c.Names {
		if len(name) > 0 {
			n := name
			if n[0] == '/' {
				n = n[1:]
			}
			return n
		}
	}
	return c.ID[:12]
}
