package hook

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jrsearles/caddy-dev-local/discovery"
)

type recordingHandler struct {
	mu      sync.Mutex
	level   slog.Level
	records []slog.Record
}

func (h *recordingHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *recordingHandler) Handle(_ context.Context, record slog.Record) error { //nolint:gocritic
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, record.Clone())
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *recordingHandler) WithGroup(string) slog.Handler {
	return h
}

func (h *recordingHandler) Records() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.records)
}

func update(id string) discovery.Update {
	return discovery.Update{Snapshot: []*discovery.ContainerInfo{{ContainerID: id}}}
}

func updateID(update discovery.Update) string { //nolint:gocritic
	if len(update.Snapshot) == 0 {
		return ""
	}
	return update.Snapshot[0].ContainerID
}

func runRuntime(t *testing.T, r *Runtime, initial discovery.Update) context.CancelFunc { //nolint:gocritic
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
		t.Fatal("timed out waiting for hook")
		return ""
	}
}

func TestInitialApply(t *testing.T) {
	applied := make(chan string, 1)
	r := NewRuntime(nil)
	if err := r.Register(Func{HookName: "initial", ApplyFunc: func(_ context.Context, update discovery.Update) error {
		applied <- updateID(update)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	runRuntime(t, r, update("initial"))

	if got := receive(t, applied); got != "initial" {
		t.Fatalf("initial update = %q", got)
	}
}

func TestInitialApplyFailureStopsStartup(t *testing.T) {
	wantErr := errors.New("initial failure")
	r := NewRuntime(nil)
	if err := r.Register(Func{HookName: "broken", ApplyFunc: func(context.Context, discovery.Update) error {
		return wantErr
	}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := r.Start(ctx, update("initial")); !errors.Is(err, wantErr) {
		t.Fatalf("Start error = %v, want %v", err, wantErr)
	}
}

func TestCleanupAttemptsEveryHook(t *testing.T) {
	var cleaned []string
	wantErr := errors.New("cleanup failure")
	r := NewRuntime(nil)
	for _, p := range []Func{
		{HookName: "first", CleanupFunc: func(context.Context) error {
			cleaned = append(cleaned, "first")
			return wantErr
		}},
		{HookName: "second", CleanupFunc: func(context.Context) error {
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

func TestHookIsolation(t *testing.T) {
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	fast := make(chan string, 2)
	r := NewRuntime(nil)
	if err := r.Register(Func{HookName: "slow", ApplyFunc: func(_ context.Context, update discovery.Update) error {
		if updateID(update) == "update" {
			close(slowStarted)
			<-releaseSlow
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Func{HookName: "fast", ApplyFunc: func(_ context.Context, update discovery.Update) error {
		fast <- updateID(update)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	runRuntime(t, r, update("initial"))
	if got := receive(t, fast); got != "initial" {
		t.Fatalf("initial update = %q", got)
	}

	r.Submit(update("update"))
	select {
	case <-slowStarted:
	case <-time.After(time.Second):
		t.Fatal("slow hook did not start")
	}
	if got := receive(t, fast); got != "update" {
		t.Fatalf("fast hook update = %q", got)
	}
	close(releaseSlow)
}

func TestLatestWinsCoalescing(t *testing.T) {
	applied := make(chan string, 3)
	block := make(chan struct{})
	started := make(chan struct{})
	r := NewRuntime(nil)
	if err := r.Register(Func{HookName: "coalesce", ApplyFunc: func(_ context.Context, update discovery.Update) error {
		id := updateID(update)
		applied <- id
		if id == "one" {
			close(started)
			<-block
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	runRuntime(t, r, update("initial"))
	if got := receive(t, applied); got != "initial" {
		t.Fatalf("initial update = %q", got)
	}

	r.Submit(update("one"))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("hook did not start update")
	}
	r.Submit(update("two"))
	r.Submit(update("three"))
	close(block)
	if got := receive(t, applied); got != "one" {
		t.Fatalf("first update = %q", got)
	}
	if got := receive(t, applied); got != "three" {
		t.Fatalf("coalesced update = %q", got)
	}
}

func TestErrorIsolation(t *testing.T) {
	logs := &recordingHandler{level: slog.LevelError}
	r := NewRuntime(slog.New(logs))
	good := make(chan string, 2)
	if err := r.Register(Func{HookName: "bad", ApplyFunc: func(_ context.Context, update discovery.Update) error {
		if updateID(update) == "initial" {
			return nil
		}
		return errors.New("broken")
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Func{HookName: "good", ApplyFunc: func(_ context.Context, update discovery.Update) error {
		good <- updateID(update)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	runRuntime(t, r, update("initial"))
	if got := receive(t, good); got != "initial" {
		t.Fatalf("initial update = %q", got)
	}
	r.Submit(update("update"))
	if got := receive(t, good); got != "update" {
		t.Fatalf("update = %q", got)
	}

	deadline := time.Now().Add(time.Second)
	for len(logs.Records()) < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	records := logs.Records()
	if len(records) != 1 {
		t.Fatalf("error logs = %d, want 1", len(records))
	}
	var got any
	records[0].Attrs(func(attr slog.Attr) bool {
		if attr.Key == "hook" {
			got = attr.Value.Any()
			return false
		}
		return true
	})
	if got != "bad" {
		t.Fatalf("logged hook = %v", got)
	}
}

func TestCancellation(t *testing.T) {
	initialApplied := make(chan struct{})
	started := make(chan struct{})
	stopped := make(chan struct{})
	var once sync.Once
	r := NewRuntime(nil)
	if err := r.Register(Func{HookName: "cancel", ApplyFunc: func(ctx context.Context, update discovery.Update) error {
		if updateID(update) == "initial" {
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
	cancel := runRuntime(t, r, update("initial"))
	<-initialApplied
	r.Submit(update("update"))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("hook did not start")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("hook did not receive cancellation")
	}
}
