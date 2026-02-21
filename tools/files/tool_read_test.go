package files

import (
	"context"
	"errors"
	"testing"
)

func TestReadFileTool(t *testing.T) {
	fs := fakeFS{files: map[string][]byte{"/file.txt": []byte("hello")}}
	tool := NewReadFileToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{"path": "/file.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello" {
		t.Fatalf("expected %q, got %q", "hello", out)
	}
}

func TestReadFileToolValidationErrors(t *testing.T) {
	tool := NewReadFileToolWithFS(fakeFS{})

	out, err := tool.Call(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: missing required argument: path" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"path": 123})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: path must be a string" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"path": "  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: path cannot be empty" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestReadFileToolMissingFS(t *testing.T) {
	tool := NewReadFileToolWithFS(nil)
	out, err := tool.Call(context.Background(), map[string]any{"path": "/file.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: filesystem not configured" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestReadFileToolReadError(t *testing.T) {
	expected := errors.New("read fail")
	fs := fakeFS{readFileErr: map[string]error{"/file.txt": expected}}
	tool := NewReadFileToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{"path": "/file.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: read fail" {
		t.Fatalf("unexpected output: %q", out)
	}
}
