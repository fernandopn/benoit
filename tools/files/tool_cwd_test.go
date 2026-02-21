package files

import (
	"context"
	"testing"
)

func TestCurrentDirectoryTool(t *testing.T) {
	fs := fakeFS{cwd: "/work"}
	tool := NewCurrentDirectoryToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "/work" {
		t.Fatalf("expected %q, got %q", "/work", out)
	}
}

func TestCurrentDirectoryToolMissingFS(t *testing.T) {
	tool := NewCurrentDirectoryToolWithFS(nil)
	out, err := tool.Call(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: filesystem not configured" {
		t.Fatalf("unexpected output: %q", out)
	}
}
