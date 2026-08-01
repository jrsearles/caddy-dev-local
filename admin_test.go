package caddydevlocal

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestComputePatch(t *testing.T) {
	prev := map[string]json.RawMessage{
		"a": json.RawMessage(`{"x":1}`),
		"b": json.RawMessage(`{"x":2}`),
		"c": json.RawMessage(`{"x":3}`),
	}
	desired := map[string]json.RawMessage{
		"b": json.RawMessage(`{"x":2}`),
		"c": json.RawMessage(`{"x":4}`),
		"d": json.RawMessage(`{"x":5}`),
	}

	removed, updated, added := computePatch(prev, desired)

	if want := map[string]json.RawMessage{"a": json.RawMessage(`{"x":1}`)}; !reflect.DeepEqual(removed, want) {
		t.Errorf("removed = %v, want %v", removed, want)
	}
	if want := map[string]json.RawMessage{"c": json.RawMessage(`{"x":4}`)}; !reflect.DeepEqual(updated, want) {
		t.Errorf("updated = %v, want %v", updated, want)
	}
	if want := map[string]json.RawMessage{"d": json.RawMessage(`{"x":5}`)}; !reflect.DeepEqual(added, want) {
		t.Errorf("added = %v, want %v", added, want)
	}
}

func TestComputePatchUnchanged(t *testing.T) {
	prev := map[string]json.RawMessage{
		"a": json.RawMessage(`{"x":1}`),
	}
	desired := map[string]json.RawMessage{
		"a": json.RawMessage(`{"x":1}`),
	}

	removed, updated, added := computePatch(prev, desired)
	if len(removed) != 0 || len(updated) != 0 || len(added) != 0 {
		t.Errorf("expected no changes, got removed=%v updated=%v added=%v", removed, updated, added)
	}
}

func TestComputePatchAllAdded(t *testing.T) {
	prev := map[string]json.RawMessage{}
	desired := map[string]json.RawMessage{
		"a": json.RawMessage(`{"x":1}`),
	}

	removed, updated, added := computePatch(prev, desired)
	if len(removed) != 0 || len(updated) != 0 {
		t.Errorf("expected no removals/updates, got removed=%v updated=%v", removed, updated)
	}
	if !reflect.DeepEqual(added, desired) {
		t.Errorf("added = %v, want %v", added, desired)
	}
}
