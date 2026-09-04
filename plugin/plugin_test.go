package plugin

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/jrsearles/caddy-dev-local/discovery"
)

func delta(id string) discovery.Delta {
	return discovery.Delta{Snapshot: []*discovery.ContainerInfo{{ContainerID: id}}}
}

func deltaID(d discovery.Delta) string { //nolint:gocritic
	if len(d.Snapshot) == 0 {
		return ""
	}
	return d.Snapshot[0].ContainerID
}

func runRuntime(t *testing.T, r *Runtime, initial discovery.Delta) context.CancelFunc { //nolint:gocritic
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, initial) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Run did not stop")
		}
	})
	return cancel
}

func receive(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for plugin")
		return ""
	}
}

func TestInitialApply(t *testing.T) {
	applied := make(chan string, 1)
	r := NewRuntime(nil)
	if err := r.Register(Func{PluginName: "initial", ApplyFunc: func(_ context.Context, d discovery.Delta) error {
		applied <- deltaID(d)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	runRuntime(t, r, delta("initial"))

	if got := receive(t, applied); got != "initial" {
		t.Fatalf("initial delta = %q", got)
	}
}

func TestInitialApplyFailureStopsStartup(t *testing.T) {
	wantErr := errors.New("initial failure")
	r := NewRuntime(nil)
	if err := r.Register(Func{PluginName: "broken", ApplyFunc: func(context.Context, discovery.Delta) error {
		return wantErr
	}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := r.Start(ctx, delta("initial")); !errors.Is(err, wantErr) {
		t.Fatalf("Start error = %v, want %v", err, wantErr)
	}
}

func TestCleanupAttemptsEveryPlugin(t *testing.T) {
	var cleaned []string
	wantErr := errors.New("cleanup failure")
	r := NewRuntime(nil)
	for _, p := range []Func{
		{PluginName: "first", CleanupFunc: func(context.Context) error {
			cleaned = append(cleaned, "first")
			return wantErr
		}},
		{PluginName: "second", CleanupFunc: func(context.Context) error {
			cleaned = append(cleaned, "second")
			return nil
		}},
	} {
		if err := r.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Cleanup(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Cleanup error = %v, want %v", err, wantErr)
	}
	if !slices.Equal(cleaned, []string{"first", "second"}) {
		t.Fatalf("cleaned = %v", cleaned)
	}
}

func TestPluginIsolation(t *testing.T) {
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	fast := make(chan string, 2)
	r := NewRuntime(nil)
	if err := r.Register(Func{PluginName: "slow", ApplyFunc: func(_ context.Context, d discovery.Delta) error {
		if deltaID(d) == "update" {
			close(slowStarted)
			<-releaseSlow
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Func{PluginName: "fast", ApplyFunc: func(_ context.Context, d discovery.Delta) error {
		fast <- deltaID(d)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	runRuntime(t, r, delta("initial"))
	if got := receive(t, fast); got != "initial" {
		t.Fatalf("initial delta = %q", got)
	}

	r.Submit(delta("update"))
	select {
	case <-slowStarted:
	case <-time.After(time.Second):
		t.Fatal("slow plugin did not start")
	}
	if got := receive(t, fast); got != "update" {
		t.Fatalf("fast plugin delta = %q", got)
	}
	close(releaseSlow)
}

func TestLatestWinsCoalescing(t *testing.T) {
	applied := make(chan string, 3)
	block := make(chan struct{})
	started := make(chan struct{})
	r := NewRuntime(nil)
	if err := r.Register(Func{PluginName: "coalesce", ApplyFunc: func(_ context.Context, d discovery.Delta) error {
		id := deltaID(d)
		applied <- id
		if id == "one" {
			close(started)
			<-block
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	runRuntime(t, r, delta("initial"))
	if got := receive(t, applied); got != "initial" {
		t.Fatalf("initial delta = %q", got)
	}

	r.Submit(delta("one"))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("plugin did not start update")
	}
	r.Submit(delta("two"))
	r.Submit(delta("three"))
	close(block)
	if got := receive(t, applied); got != "one" {
		t.Fatalf("first update = %q", got)
	}
	if got := receive(t, applied); got != "three" {
		t.Fatalf("coalesced update = %q", got)
	}
}

func TestErrorIsolation(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	r := NewRuntime(zap.New(core))
	good := make(chan string, 2)
	if err := r.Register(Func{PluginName: "bad", ApplyFunc: func(_ context.Context, d discovery.Delta) error {
		if deltaID(d) == "initial" {
			return nil
		}
		return errors.New("broken")
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Func{PluginName: "good", ApplyFunc: func(_ context.Context, d discovery.Delta) error {
		good <- deltaID(d)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	runRuntime(t, r, delta("initial"))
	if got := receive(t, good); got != "initial" {
		t.Fatalf("initial delta = %q", got)
	}
	r.Submit(delta("update"))
	if got := receive(t, good); got != "update" {
		t.Fatalf("update delta = %q", got)
	}

	deadline := time.Now().Add(time.Second)
	for logs.Len() < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if logs.Len() != 1 {
		t.Fatalf("error logs = %d, want 1", logs.Len())
	}
	if got := logs.All()[0].ContextMap()["plugin"]; got != "bad" {
		t.Fatalf("logged plugin = %v", got)
	}
}

func TestCancellation(t *testing.T) {
	initialApplied := make(chan struct{})
	started := make(chan struct{})
	stopped := make(chan struct{})
	var once sync.Once
	r := NewRuntime(nil)
	if err := r.Register(Func{PluginName: "cancel", ApplyFunc: func(ctx context.Context, d discovery.Delta) error {
		if deltaID(d) == "initial" {
			close(initialApplied)
			return nil
		}
		once.Do(func() { close(started) })
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}}); err != nil {
		t.Fatal(err)
	}
	cancel := runRuntime(t, r, delta("initial"))
	<-initialApplied
	r.Submit(delta("update"))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("plugin did not start")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("plugin did not receive cancellation")
	}
}
