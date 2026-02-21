package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileTool(t *testing.T) {
	fs := fakeFS{files: map[string][]byte{}}
	tool := NewWriteFileToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{"filePath": "/file.txt", "content": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "wrote 5 bytes to /file.txt" {
		t.Fatalf("unexpected output: %q", out)
	}
	if got := string(fs.files["/file.txt"]); got != "hello" {
		t.Fatalf("unexpected written content: %q", got)
	}
}

func TestWriteFileToolValidationErrors(t *testing.T) {
	tool := NewWriteFileToolWithFS(fakeFS{files: map[string][]byte{}})

	out, err := tool.Call(context.Background(), map[string]any{"content": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: missing required argument: filePath" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"filePath": 7, "content": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: filePath must be a string" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"filePath": "/file.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: missing required argument: content" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestWriteFileToolMissingFS(t *testing.T) {
	tool := NewWriteFileToolWithFS(nil)
	out, err := tool.Call(context.Background(), map[string]any{"filePath": "/file.txt", "content": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: filesystem not configured" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestWriteFileToolWriteError(t *testing.T) {
	fs := fakeFS{writeFileErr: map[string]error{"/file.txt": errors.New("write fail")}, files: map[string][]byte{}}
	tool := NewWriteFileToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{"filePath": "/file.txt", "content": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: write fail" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestWriteFileToolRestrictedFS(t *testing.T) {
	base := fakeFS{files: map[string][]byte{}}
	restricted, err := NewRestrictedFSWithBaseAndRoots(base, []string{"/root"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tool := NewWriteFileToolWithFS(restricted)
	out, err := tool.Call(context.Background(), map[string]any{"filePath": "/etc/passwd", "content": "nope"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: path outside allowed root: /root" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestWriteFileToolCreatesDirectoriesOnDisk(t *testing.T) {
	root := t.TempDir()
	sandboxFS, err := NewChrootFS(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tool := NewWriteFileToolWithFS(sandboxFS)
	out, err := tool.Call(context.Background(), map[string]any{"filePath": "/dir/nested/file.txt", "content": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "wrote 5 bytes to /dir/nested/file.txt" {
		t.Fatalf("unexpected output: %q", out)
	}
	data, err := os.ReadFile(filepath.Join(root, "dir", "nested", "file.txt"))
	if err != nil {
		t.Fatalf("expected written file, got error: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected file contents: %q", string(data))
	}
}

func TestWriteFileToolCreateDirectoriesError(t *testing.T) {
	base := fakeFS{
		files:       map[string][]byte{},
		mkdirAllErr: map[string]error{"/sandbox/dir/nested": errors.New("mkdir fail")},
	}
	sandboxFS, err := NewChrootFSWithBase(base, "/sandbox")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tool := NewWriteFileToolWithFS(sandboxFS)
	out, err := tool.Call(context.Background(), map[string]any{"filePath": "/dir/nested/file.txt", "content": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: mkdir fail" {
		t.Fatalf("unexpected output: %q", out)
	}
}
