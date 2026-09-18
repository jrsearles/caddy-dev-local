package hook

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jrsearles/caddy-dev-local/discovery"
)

func TestSequenceOrdersApplyAndCleanup(t *testing.T) {
	var applied, cleaned []string
	makeHook := func(name string) Func {
		return Func{
			HookName: name,
			ApplyFunc: func(context.Context, discovery.Update) error {
				applied = append(applied, name)
				return nil
			},
			CleanupFunc: func(context.Context) error {
				cleaned = append(cleaned, name)
				return nil
			},
		}
	}
	sequence := NewSequence("ordered", makeHook("first"), makeHook("second"))
	if err := sequence.Apply(context.Background(), discovery.Update{}); err != nil {
		t.Fatal(err)
	}
	if err := sequence.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(applied, []string{"first", "second"}) {
		t.Fatalf("applied = %v", applied)
	}
	if !slices.Equal(cleaned, []string{"first", "second"}) {
		t.Fatalf("cleaned = %v", cleaned)
	}
}

func TestSequenceStopsApplyButContinuesCleanupOnError(t *testing.T) {
	wantErr := errors.New("failure")
	secondApplied := false
	secondCleaned := false
	sequence := NewSequence("ordered",
		Func{HookName: "first", ApplyFunc: func(context.Context, discovery.Update) error { return wantErr }, CleanupFunc: func(context.Context) error { return wantErr }},
		Func{HookName: "second", ApplyFunc: func(context.Context, discovery.Update) error { secondApplied = true; return nil }, CleanupFunc: func(context.Context) error { secondCleaned = true; return nil }},
	)
	if err := sequence.Apply(context.Background(), discovery.Update{}); !errors.Is(err, wantErr) {
		t.Fatalf("Apply error = %v", err)
	}
	if secondApplied {
		t.Fatal("second hook applied after dependency failure")
	}
	if err := sequence.Cleanup(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Cleanup error = %v", err)
	}
	if !secondCleaned {
		t.Fatal("second hook cleanup was skipped")
	}
}
