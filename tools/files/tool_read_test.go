package files

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestReadFileTool(t *testing.T) {
	fs := fakeFS{files: map[string][]byte{"/file.txt": []byte("hello\nworld")}}
	tool := NewReadFileToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{"filePath": "/file.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "1: hello\n2: world" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestReadFileToolDirectory(t *testing.T) {
	fs := fakeFS{entries: map[string][]os.DirEntry{
		"/root": {fakeDirEntry{name: "a.txt"}, fakeDirEntry{name: "sub", dir: true}},
	}}
	tool := NewReadFileToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{"filePath": "/root"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "a.txt\nsub/" {
		t.Fatalf("unexpected directory output: %q", out)
	}
}

func TestReadFileToolValidationErrors(t *testing.T) {
	tool := NewReadFileToolWithFS(fakeFS{})

	out, err := tool.Call(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: missing required argument: filePath" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"filePath": 123})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: filePath must be a string" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"filePath": "  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: filePath cannot be empty" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"filePath": "/file.txt", "offset": 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: offset must be greater than zero" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"filePath": "/file.txt", "limit": "abc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: limit must be an integer" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestReadFileToolMissingFS(t *testing.T) {
	tool := NewReadFileToolWithFS(nil)
	out, err := tool.Call(context.Background(), map[string]any{"filePath": "/file.txt"})
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
	out, err := tool.Call(context.Background(), map[string]any{"filePath": "/file.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: read fail" {
		t.Fatalf("unexpected output: %q", out)
	}
}
