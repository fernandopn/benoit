package files

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestListFilesToolListsSortedEntries(t *testing.T) {
	fs := fakeFS{entries: map[string][]os.DirEntry{
		"/root": {
			fakeDirEntry{name: "b.txt"},
			fakeDirEntry{name: "a.txt"},
			fakeDirEntry{name: "dir", dir: true},
		},
	}}
	tool := NewListFilesToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{"path": "/root"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := strings.Join([]string{"a.txt", "b.txt", "dir/"}, "\n")
	if out != want {
		t.Fatalf("unexpected output:\nwant: %q\n got: %q", want, out)
	}
}

func TestListFilesToolDefaultPath(t *testing.T) {
	fs := fakeFS{entries: map[string][]os.DirEntry{
		".": {fakeDirEntry{name: "file.txt"}},
	}}
	tool := NewListFilesToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "file.txt" {
		t.Fatalf("expected output to include file.txt, got %q", out)
	}
}

func TestListFilesToolValidationErrors(t *testing.T) {
	tool := NewListFilesToolWithFS(fakeFS{})

	out, err := tool.Call(context.Background(), map[string]any{"path": 123})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: path must be a string" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"path": "   "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: path cannot be empty" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestListFilesToolMissingFS(t *testing.T) {
	tool := NewListFilesToolWithFS(nil)
	out, err := tool.Call(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: filesystem not configured" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestListFilesToolReadDirError(t *testing.T) {
	expected := errors.New("boom")
	fs := fakeFS{readDirErr: map[string]error{"/root": expected}}
	tool := NewListFilesToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{"path": "/root"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: boom" {
		t.Fatalf("unexpected output: %q", out)
	}
}
