package files

import (
	"errors"
	"os"
	"testing"
)

func TestRestrictedFSEmptyRootError(t *testing.T) {
	base := fakeFS{cwd: "/root"}
	if _, err := NewRestrictedFSWithBase(base, ""); err == nil {
		t.Fatal("expected error for empty root")
	}
}

func TestRestrictedFSDeniesOutsideRoot(t *testing.T) {
	base := fakeFS{cwd: "/root"}
	fs, err := NewRestrictedFSWithBase(base, "/root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = fs.ReadDir("/etc")
	if err == nil {
		t.Fatal("expected error for outside root")
	}
	_, err = fs.ReadDir("../outside")
	if err == nil {
		t.Fatal("expected error for relative outside root")
	}
}

func TestRestrictedFSAllowsAbsoluteWithinRoot(t *testing.T) {
	base := fakeFS{cwd: "/root", entries: map[string][]os.DirEntry{
		"/root/sub": {},
	}}
	fs, err := NewRestrictedFSWithBase(base, "/root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := fs.ReadDir("/root/sub"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRestrictedFSEmptyPath(t *testing.T) {
	base := fakeFS{cwd: "/root"}
	fs, err := NewRestrictedFSWithBase(base, "/root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = fs.ReadDir("  ")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestRestrictedFSReadFileResolvesWithinRoot(t *testing.T) {
	base := fakeFS{cwd: "/root", files: map[string][]byte{"/root/file.txt": []byte("ok")}}
	fs, err := NewRestrictedFSWithBase(base, "/root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := fs.ReadFile("file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "ok" {
		t.Fatalf("expected ok, got %q", string(data))
	}
}

func TestRestrictedFSPropagatesBaseError(t *testing.T) {
	base := fakeFS{cwd: "/root", readDirErr: map[string]error{"/root": errors.New("boom")}}
	fs, err := NewRestrictedFSWithBase(base, "/root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = fs.ReadDir("/root")
	if err == nil {
		t.Fatal("expected error")
	}
}
