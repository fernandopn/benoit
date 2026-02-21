package files

import (
	"context"
	"errors"
	"testing"
)

func TestPatchFileToolAddFile(t *testing.T) {
	fs := fakeFS{files: map[string][]byte{}}
	tool := NewPatchFileToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{
		"patchText": "*** Begin Patch\n*** Add File: /file.txt\n+hello\n+world\n*** End Patch",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "applied patch with 1 operation(s)" {
		t.Fatalf("unexpected output: %q", out)
	}
	if got := string(fs.files["/file.txt"]); got != "hello\nworld" {
		t.Fatalf("unexpected created content: %q", got)
	}
}

func TestPatchFileToolUpdateFile(t *testing.T) {
	fs := fakeFS{files: map[string][]byte{"/file.txt": []byte("hello\nworld")}}
	tool := NewPatchFileToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{
		"patchText": "*** Begin Patch\n*** Update File: /file.txt\n@@ -1,2 +1,2 @@\n-hello\n+bye\n world\n*** End Patch",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "applied patch with 1 operation(s)" {
		t.Fatalf("unexpected output: %q", out)
	}
	if got := string(fs.files["/file.txt"]); got != "bye\nworld" {
		t.Fatalf("unexpected patched content: %q", got)
	}
}

func TestPatchFileToolDeleteAndMove(t *testing.T) {
	fs := fakeFS{files: map[string][]byte{"/old.txt": []byte("a"), "/tmp.txt": []byte("x")}}
	tool := NewPatchFileToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{
		"patchText": "*** Begin Patch\n*** Update File: /old.txt\n*** Move to: /new.txt\n@@ -1,1 +1,1 @@\n-a\n+b\n*** Delete File: /tmp.txt\n*** End Patch",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "applied patch with 2 operation(s)" {
		t.Fatalf("unexpected output: %q", out)
	}
	if _, exists := fs.files["/old.txt"]; exists {
		t.Fatal("expected old file to be removed after move")
	}
	if got := string(fs.files["/new.txt"]); got != "b" {
		t.Fatalf("unexpected moved content: %q", got)
	}
	if _, exists := fs.files["/tmp.txt"]; exists {
		t.Fatal("expected deleted file to be removed")
	}
}

func TestPatchFileToolValidationErrors(t *testing.T) {
	fs := fakeFS{files: map[string][]byte{"/file.txt": []byte("hello")}}
	tool := NewPatchFileToolWithFS(fs)

	out, err := tool.Call(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: missing required argument: patchText" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"patchText": 77})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: patchText must be a string" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"patchText": "   "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: missing required argument: patchText" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"patchText": "*** Begin Patch\n*** End Patch"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: patch has no operations" {
		t.Fatalf("unexpected output: %q", out)
	}

	out, err = tool.Call(context.Background(), map[string]any{"patchText": "*** Begin Patch\n*** Update File: /file.txt\n@@ -1,1 +1,1 @@\n-absent\n+x\n*** End Patch"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" || out == "applied patch with 1 operation(s)" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestPatchFileToolMissingFS(t *testing.T) {
	tool := NewPatchFileToolWithFS(nil)
	out, err := tool.Call(context.Background(), map[string]any{"patchText": "*** Begin Patch\n*** Delete File: /file.txt\n*** End Patch"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: filesystem not configured" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestPatchFileToolReadAndWriteErrors(t *testing.T) {
	readErrFS := fakeFS{readFileErr: map[string]error{"/file.txt": errors.New("read fail")}, files: map[string][]byte{}}
	tool := NewPatchFileToolWithFS(readErrFS)
	out, err := tool.Call(context.Background(), map[string]any{"patchText": "*** Begin Patch\n*** Update File: /file.txt\n@@ -1,1 +1,1 @@\n-a\n+b\n*** End Patch"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: read fail" {
		t.Fatalf("unexpected output: %q", out)
	}

	writeErrFS := fakeFS{
		files:        map[string][]byte{"/file.txt": []byte("a")},
		writeFileErr: map[string]error{"/file.txt": errors.New("write fail")},
	}
	tool = NewPatchFileToolWithFS(writeErrFS)
	out, err = tool.Call(context.Background(), map[string]any{"patchText": "*** Begin Patch\n*** Update File: /file.txt\n@@ -1,1 +1,1 @@\n-a\n+b\n*** End Patch"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: write fail" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestPatchFileToolDeleteError(t *testing.T) {
	fs := fakeFS{removeFileErr: map[string]error{"/file.txt": errors.New("delete fail")}, files: map[string][]byte{"/file.txt": []byte("x")}}
	tool := NewPatchFileToolWithFS(fs)
	out, err := tool.Call(context.Background(), map[string]any{"patchText": "*** Begin Patch\n*** Delete File: /file.txt\n*** End Patch"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: delete fail" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestPatchFileToolRestrictedFS(t *testing.T) {
	base := fakeFS{files: map[string][]byte{}}
	restricted, err := NewRestrictedFSWithBaseAndRoots(base, []string{"/root"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tool := NewPatchFileToolWithFS(restricted)
	out, err := tool.Call(context.Background(), map[string]any{"patchText": "*** Begin Patch\n*** Add File: /etc/passwd\n+nope\n*** End Patch"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: path outside allowed root: /root" {
		t.Fatalf("unexpected output: %q", out)
	}
}
