package tools

import (
	"context"
	"testing"
	"time"
)

func TestClockToolReturnsRFC3339(t *testing.T) {
	tool := NewClockTool()
	out, err := tool.Call(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty time string")
	}
	if _, err := time.Parse(time.RFC3339, out); err != nil {
		t.Fatalf("expected RFC3339 time, got %q: %v", out, err)
	}
}

func TestClockToolWithNow(t *testing.T) {
	fixed := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	tool := NewClockToolWithNow(func() time.Time { return fixed })
	out, err := tool.Call(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != fixed.Format(time.RFC3339) {
		t.Fatalf("expected %q, got %q", fixed.Format(time.RFC3339), out)
	}
}
