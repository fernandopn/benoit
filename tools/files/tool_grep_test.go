package files

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestGrepToolMatches(t *testing.T) {
	fs := fakeFS{
		entries: map[string][]os.DirEntry{
			"/root":     {fakeDirEntry{name: "a.go"}, fakeDirEntry{name: "b.txt"}, fakeDirEntry{name: "sub", dir: true}},
			"/root/sub": {fakeDirEntry{name: "c.go"}},
		},
		files: map[string][]byte{
			"/root/a.go":     []byte("alpha\nneedle"),
			"/root/b.txt":    []byte("none"),
			"/root/sub/c.go": []byte("needle again"),
		},
	}
	tool := NewGrepToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{"pattern": "needle", "path": "/root"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "/root/a.go:2\n/root/sub/c.go:1" {
		t.Fatalf("unexpected grep output: %q", out)
	}
}

func TestGrepToolIncludeFilter(t *testing.T) {
	fs := fakeFS{
		entries: map[string][]os.DirEntry{
			"/root": {fakeDirEntry{name: "a.go"}, fakeDirEntry{name: "b.txt"}},
		},
		files: map[string][]byte{
			"/root/a.go":  []byte("needle"),
			"/root/b.txt": []byte("needle"),
		},
	}
	tool := NewGrepToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{"pattern": "needle", "path": "/root", "include": "*.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "/root/a.go:1" {
		t.Fatalf("unexpected grep output: %q", out)
	}
}

func TestGrepToolValidationAndErrors(t *testing.T) {
	tool := NewGrepToolWithFS(fakeFS{})

	out, err := tool.Call(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: missing required argument: pattern" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"pattern": "["})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" || out == "error" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"pattern": "ok", "include": 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: include must be a string" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestGrepToolMissingFSAndReadError(t *testing.T) {
	tool := NewGrepToolWithFS(nil)
	out, err := tool.Call(context.Background(), map[string]any{"pattern": "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: filesystem not configured" {
		t.Fatalf("unexpected output: %q", out)
	}

	readErr := fakeFS{
		entries:     map[string][]os.DirEntry{"/root": {fakeDirEntry{name: "a.go"}}},
		readFileErr: map[string]error{"/root/a.go": errors.New("read fail")},
	}
	tool = NewGrepToolWithFS(readErr)
	out, err = tool.Call(context.Background(), map[string]any{"pattern": "x", "path": "/root"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "" {
		t.Fatalf("expected empty output when files fail to read, got %q", out)
	}
}
