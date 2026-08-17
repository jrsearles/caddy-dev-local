package discovery

import (
	"testing"
	"time"
)

func TestStatusInitial(t *testing.T) {
	c := &Controller{}
	s := c.Status()
	if s.LastError != "" {
		t.Errorf("expected empty LastError, got %q", s.LastError)
	}
	if !s.LastRefresh.IsZero() {
		t.Error("expected zero LastRefresh")
	}
}

func TestSetError(t *testing.T) {
	c := &Controller{}
	before := time.Now()
	c.setError("something broke")
	after := time.Now()

	s := c.Status()
	if s.LastError != "something broke" {
		t.Errorf("expected %q, got %q", "something broke", s.LastError)
	}
	if s.LastErrorAt.Before(before) || s.LastErrorAt.After(after) {
		t.Error("LastErrorAt out of expected range")
	}
	if !s.LastRefresh.IsZero() {
		t.Error("LastRefresh should still be zero after setError")
	}
}

func TestClearError(t *testing.T) {
	c := &Controller{}
	c.setError("oops")
	before := time.Now()
	c.clearError()
	after := time.Now()

	s := c.Status()
	if s.LastError != "" {
		t.Errorf("expected empty LastError after clear, got %q", s.LastError)
	}
	if s.LastErrorAt != (time.Time{}) {
		t.Error("LastErrorAt should be zero after clear")
	}
	if s.LastRefresh.Before(before) || s.LastRefresh.After(after) {
		t.Error("LastRefresh not updated on clearError")
	}
}

func TestSetApply(t *testing.T) {
	called := false
	c := &Controller{}
	c.SetApply(func() { called = true })
	c.apply()
	if !called {
		t.Error("SetApply did not wire the apply function")
	}
}
