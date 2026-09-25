package controller

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"

	"github.com/jrsearles/caddy-dev-local/config"
)

type recordingHandler struct {
	messages []string
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, record slog.Record) error { //nolint:gocritic
	h.messages = append(h.messages, record.Message)
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *recordingHandler) WithGroup(string) slog.Handler { return h }

type mockDocker struct {
	listErr    error
	listCalls  atomic.Int32
	eventCalls atomic.Int32
}

func (m *mockDocker) ContainerList(context.Context, client.ContainerListOptions) ([]container.Summary, error) {
	m.listCalls.Add(1)
	return nil, m.listErr
}

func (m *mockDocker) Events(context.Context, client.EventsListOptions) (<-chan events.Message, <-chan error) {
	m.eventCalls.Add(1)
	return make(chan events.Message), make(chan error)
}

func TestRunOnceRefreshesWithoutWatchingEvents(t *testing.T) {
	docker := &mockDocker{}
	logs := &recordingHandler{}
	err := RunOnce(context.Background(), Options{
		Config:       config.DefaultConfig(),
		DockerClient: docker,
		Logger:       slog.New(logs),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := docker.listCalls.Load(); got != 1 {
		t.Fatalf("ContainerList calls = %d, want 1", got)
	}
	if got := docker.eventCalls.Load(); got != 0 {
		t.Fatalf("Events calls = %d, want 0", got)
	}
	if len(logs.messages) != 1 || logs.messages[0] != "one-pass reconciliation complete; run devlocal start to watch continuously" {
		t.Fatalf("log messages = %q", logs.messages)
	}
}

func TestRunOnceReturnsRefreshFailure(t *testing.T) {
	wantErr := errors.New("list failure")
	docker := &mockDocker{listErr: wantErr}
	err := RunOnce(context.Background(), Options{
		Config:       config.DefaultConfig(),
		DockerClient: docker,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("RunOnce error = %v, want %v", err, wantErr)
	}
	if got := docker.eventCalls.Load(); got != 0 {
		t.Fatalf("Events calls = %d, want 0", got)
	}
}

func TestRunOnceValidation(t *testing.T) {
	docker := &mockDocker{}
	if err := RunOnce(context.Background(), Options{DockerClient: docker}); err == nil {
		t.Fatal("RunOnce accepted nil config")
	}
	if err := RunOnce(context.Background(), Options{
		Config:       config.DefaultConfig(),
		DockerClient: docker,
		UI:           true,
	}); err == nil {
		t.Fatal("RunOnce accepted UI without Caddy API client")
	}
}
