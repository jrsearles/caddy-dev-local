package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"go.uber.org/zap"

	"github.com/jsearles/caddy-dev-local/config"
	"github.com/jsearles/caddy-dev-local/generator"
)

type mockEventsClient struct {
	containers []container.Summary
	msgCh      chan events.Message
	errCh      chan error
}

func (m *mockEventsClient) ContainerList(ctx context.Context, options client.ContainerListOptions) ([]container.Summary, error) {
	return m.containers, nil
}

func (m *mockEventsClient) ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error) {
	return container.InspectResponse{}, nil
}

func (m *mockEventsClient) Events(ctx context.Context, options client.EventsListOptions) (<-chan events.Message, <-chan error) {
	return m.msgCh, m.errCh
}

func (m *mockEventsClient) NetworkInspect(ctx context.Context, networkID string) (network.Inspect, error) {
	return network.Inspect{}, nil
}

func TestShouldRefresh(t *testing.T) {
	cfg := &config.Config{IngressNetwork: "devlocal"}

	cases := []struct {
		name  string
		event events.Message
		want  bool
	}{
		{"container start", events.Message{Type: events.ContainerEventType, Action: events.ActionStart}, true},
		{"container stop", events.Message{Type: events.ContainerEventType, Action: events.ActionStop}, true},
		{"container die", events.Message{Type: events.ContainerEventType, Action: events.ActionDie}, true},
		{"container destroy", events.Message{Type: events.ContainerEventType, Action: events.ActionDestroy}, true},
		{"container create", events.Message{Type: events.ContainerEventType, Action: events.ActionCreate}, true},
		{"container pause", events.Message{Type: events.ContainerEventType, Action: "pause"}, false},
		{"network connect matching", events.Message{Type: events.NetworkEventType, Action: events.ActionConnect, Actor: events.Actor{Attributes: map[string]string{"name": "devlocal"}}}, true},
		{"network disconnect matching", events.Message{Type: events.NetworkEventType, Action: events.ActionDisconnect, Actor: events.Actor{Attributes: map[string]string{"name": "devlocal"}}}, true},
		{"network connect other", events.Message{Type: events.NetworkEventType, Action: events.ActionConnect, Actor: events.Actor{Attributes: map[string]string{"name": "other"}}}, false},
		{"unrelated", events.Message{Type: "image", Action: "pull"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRefresh(&tc.event, cfg); got != tc.want {
				t.Errorf("shouldRefresh(%v) = %v, want %v", tc.event, got, tc.want)
			}
		})
	}
}

func TestWatchEventsRefreshAndApply(t *testing.T) {
	cfg := &config.Config{IngressNetwork: "devlocal", TLD: "dev.local", Standalone: true}
	mock := &mockEventsClient{
		msgCh: make(chan events.Message, 4),
		errCh: make(chan error),
	}
	gen := generator.NewGenerator(cfg, mock)

	refreshCh := make(chan struct{}, 1)
	applyCh := make(chan struct{}, 1)
	refresh := func(context.Context) error {
		refreshCh <- struct{}{}
		return nil
	}
	apply := func() {
		applyCh <- struct{}{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	controller := New(cfg, mock, gen, refresh, apply, zap.NewNop())
	controller.Run(ctx)
	defer cancel()

	mock.msgCh <- events.Message{Type: events.ContainerEventType, Action: events.ActionStart}

	select {
	case <-applyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("apply not called after container event")
	}
	select {
	case <-refreshCh:
	default:
		t.Error("refresh not called after container event")
	}
}

func TestWatchEventsSkipsIrrelevantEvents(t *testing.T) {
	cfg := &config.Config{IngressNetwork: "devlocal", TLD: "dev.local", Standalone: true}
	mock := &mockEventsClient{
		msgCh: make(chan events.Message, 4),
		errCh: make(chan error),
	}
	gen := generator.NewGenerator(cfg, mock)

	applyCh := make(chan struct{}, 1)
	apply := func() {
		applyCh <- struct{}{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	controller := New(cfg, mock, gen, func(context.Context) error { return nil }, apply, zap.NewNop())
	controller.Run(ctx)
	defer cancel()

	mock.msgCh <- events.Message{Type: events.ContainerEventType, Action: "pause"}

	select {
	case <-applyCh:
		t.Fatal("apply called for irrelevant event")
	case <-time.After(250 * time.Millisecond):
	}
}

func TestPollLoopRefreshesAndApplies(t *testing.T) {
	cfg := &config.Config{IngressNetwork: "devlocal", TLD: "dev.local", Standalone: true, PollInterval: 10 * time.Millisecond}
	mock := &mockEventsClient{
		msgCh: make(chan events.Message, 4),
		errCh: make(chan error),
	}
	gen := generator.NewGenerator(cfg, mock)

	applyCh := make(chan struct{}, 1)
	apply := func() {
		applyCh <- struct{}{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	controller := New(cfg, mock, gen, func(context.Context) error { return nil }, apply, zap.NewNop())
	controller.Run(ctx)
	defer cancel()

	select {
	case <-applyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("apply not called on poll tick")
	}
}
