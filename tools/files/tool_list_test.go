package files

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestListFilesToolGlobMatches(t *testing.T) {
	fs := fakeFS{entries: map[string][]os.DirEntry{
		"/root": {
			fakeDirEntry{name: "a.go"},
			fakeDirEntry{name: "b.txt"},
			fakeDirEntry{name: "dir", dir: true},
		},
		"/root/dir": {
			fakeDirEntry{name: "c.go"},
		},
	}}
	tool := NewListFilesToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{"pattern": "**/*.go", "path": "/root"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "/root/a.go\n/root/dir/c.go" {
		t.Fatalf("unexpected glob output: %q", out)
	}
}

func TestListFilesToolGlobWithBraces(t *testing.T) {
	fs := fakeFS{entries: map[string][]os.DirEntry{
		"/root": {fakeDirEntry{name: "a.go"}, fakeDirEntry{name: "a.txt"}},
	}}
	tool := NewListFilesToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{"pattern": "*.{go,txt}", "path": "/root"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "/root/a.go\n/root/a.txt" {
		t.Fatalf("unexpected glob output: %q", out)
	}
}

func TestListFilesToolValidationErrors(t *testing.T) {
	tool := NewListFilesToolWithFS(fakeFS{})

	out, err := tool.Call(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: missing required argument: pattern" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"pattern": "**/*.go", "path": 123})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: path must be a string" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"pattern": "   "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: pattern cannot be empty" {
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
	fs := fakeFS{readDirErr: map[string]error{"/root": expected}, readFileErr: map[string]error{"/root": expected}}
	tool := NewListFilesToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{"pattern": "*", "path": "/root"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: boom" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestListFilesToolGlobStarWithChroot(t *testing.T) {
	base := fakeFS{entries: map[string][]os.DirEntry{
		"/sandbox": {
			fakeDirEntry{name: "a.txt"},
			fakeDirEntry{name: "dir", dir: true},
		},
		"/sandbox/dir": {
			fakeDirEntry{name: "nested", dir: true},
			fakeDirEntry{name: "b.txt"},
		},
		"/sandbox/dir/nested": {
			fakeDirEntry{name: "c.txt"},
		},
	}}
	sandboxFS, err := NewChrootFSWithBase(base, "/sandbox")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tool := NewListFilesToolWithFS(sandboxFS)
	out, err := tool.Call(context.Background(), map[string]any{"pattern": "*", "path": "/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "/a.txt\n/dir" {
		t.Fatalf("unexpected glob output: %q", out)
	}
}

func TestListFilesToolGlobSingleDirRecursiveWithChroot(t *testing.T) {
	base := fakeFS{entries: map[string][]os.DirEntry{
		"/sandbox": {
			fakeDirEntry{name: "a.txt"},
			fakeDirEntry{name: "dir", dir: true},
		},
		"/sandbox/dir": {
			fakeDirEntry{name: "nested", dir: true},
			fakeDirEntry{name: "b.txt"},
		},
		"/sandbox/dir/nested": {
			fakeDirEntry{name: "c.txt"},
		},
	}}
	sandboxFS, err := NewChrootFSWithBase(base, "/sandbox")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tool := NewListFilesToolWithFS(sandboxFS)
	out, err := tool.Call(context.Background(), map[string]any{"pattern": "*/**", "path": "/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "/dir/b.txt\n/dir/nested\n/dir/nested/c.txt" {
		t.Fatalf("unexpected glob output: %q", out)
	}
}

func TestListFilesToolMissingPathIncludesSandboxHint(t *testing.T) {
	base := fakeFS{entries: map[string][]os.DirEntry{
		"/sandbox": {
			fakeDirEntry{name: "tool_test.txt"},
		},
	}}
	sandboxFS, err := NewChrootFSWithBase(base, "/sandbox")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tool := NewListFilesToolWithFS(sandboxFS)
	out, err := tool.Call(context.Background(), map[string]any{"pattern": "**/*", "path": "/mnt/data"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "error: path not found: /mnt/data (sandbox root is /); try path \"/\" with pattern \"*\""
	if out != want {
		t.Fatalf("unexpected output: %q", out)
	}
}
