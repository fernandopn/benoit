package tools

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeFS struct {
	entries     map[string][]os.DirEntry
	readDirErr  map[string]error
	files       map[string][]byte
	readFileErr map[string]error
	cwd         string
	cwdErr      error
}

func (f fakeFS) ReadDir(name string) ([]os.DirEntry, error) {
	if err, ok := f.readDirErr[name]; ok {
		return nil, err
	}
	if entries, ok := f.entries[name]; ok {
		return entries, nil
	}
	return []os.DirEntry{}, nil
}

func (f fakeFS) ReadFile(name string) ([]byte, error) {
	if err, ok := f.readFileErr[name]; ok {
		return nil, err
	}
	if data, ok := f.files[name]; ok {
		return data, nil
	}
	return []byte{}, nil
}

func (f fakeFS) Getwd() (string, error) {
	if f.cwdErr != nil {
		return "", f.cwdErr
	}
	return f.cwd, nil
}

type fakeDirEntry struct {
	name string
	dir  bool
}

func (f fakeDirEntry) Name() string { return f.name }
func (f fakeDirEntry) IsDir() bool  { return f.dir }
func (f fakeDirEntry) Type() fs.FileMode {
	if f.dir {
		return fs.ModeDir
	}
	return 0
}
func (f fakeDirEntry) Info() (fs.FileInfo, error) {
	return fakeFileInfo{name: f.name, dir: f.dir}, nil
}

type fakeFileInfo struct {
	name string
	dir  bool
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode  { return fakeMode(f.dir) }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }

func fakeMode(dir bool) fs.FileMode {
	if dir {
		return fs.ModeDir
	}
	return 0
}

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
