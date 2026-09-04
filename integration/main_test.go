//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"go.uber.org/zap"

	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
	"github.com/jrsearles/caddy-dev-local/docker"
)

var (
	sharedDocker docker.Client
	sharedConfig *config.Config
	logger       *zap.Logger
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	logger = zap.NewNop()

	if !dockerReachable(ctx) {
		logger.Info("docker daemon not reachable, skipping integration tests")
		os.Exit(0)
	}

	api, err := docker.NewClient()
	if err != nil {
		logger.Error("creating docker client", zap.Error(err))
		os.Exit(1)
	}
	sharedDocker = api

	sharedConfig = &config.Config{
		TLD:          "dev.local",
		StaleTTL:     2 * time.Second,
		ProbeTimeout: 2 * time.Second,
		PollInterval: 0,
	}

	code := m.Run()
	os.Exit(code)
}

func dockerReachable(ctx context.Context) bool {
	c, err := client.New(client.FromEnv)
	if err != nil {
		return false
	}
	defer c.Close()
	ping, err := c.Ping(ctx, client.PingOptions{})
	if err != nil {
		return false
	}
	return ping.APIVersion != ""
}

func newDiscovery(t *testing.T) *discovery.Discovery {
	t.Helper()
	return newDiscoveryWithConfig(t, sharedConfig)
}

func newDiscoveryWithConfig(t *testing.T, cfg *config.Config) *discovery.Discovery {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d := discovery.New(cfg, sharedDocker, logger)
	d.Run(ctx)
	return d
}
