package files

import (
	"errors"
	"testing"
)

func TestNewChrootFSEmptyRootError(t *testing.T) {
	base := fakeFS{cwd: "/host"}
	if _, err := NewChrootFSWithBase(base, ""); err == nil {
		t.Fatal("expected error for empty root")
	}
}

func TestChrootFSMapsVirtualPathsToHostRoot(t *testing.T) {
	errSandbox := errors.New("sandbox")
	errHost := errors.New("host")
	base := fakeFS{readDirErr: map[string]error{
		"/sandbox/etc": errSandbox,
		"/etc":         errHost,
	}}
	fs, err := NewChrootFSWithBase(base, "/sandbox")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = fs.ReadDir("/etc")
	if !errors.Is(err, errSandbox) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChrootFSReturnsVirtualRootAsWorkingDirectory(t *testing.T) {
	fs, err := NewChrootFSWithBase(fakeFS{}, "/sandbox")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dir, err := fs.Getwd()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/" {
		t.Fatalf("unexpected cwd: %q", dir)
	}
}

func TestChrootFSWriteAndRemoveFile(t *testing.T) {
	base := fakeFS{files: map[string][]byte{}}
	fs, err := NewChrootFSWithBase(base, "/sandbox")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := fs.WriteFile("dir/file.txt", []byte("hello")); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if got := string(base.files["/sandbox/dir/file.txt"]); got != "hello" {
		t.Fatalf("unexpected written data: %q", got)
	}
	if err := fs.RemoveFile("/dir/file.txt"); err != nil {
		t.Fatalf("unexpected remove error: %v", err)
	}
	if _, exists := base.files["/sandbox/dir/file.txt"]; exists {
		t.Fatal("expected removed file")
	}
}

func TestChrootFSRootMustBeAllowedByPolicy(t *testing.T) {
	_, err := NewChrootFSWithBaseAndRoots(fakeFS{}, "/sandbox", []string{"/other"})
	if err == nil {
		t.Fatal("expected root outside allowed policy to fail")
	}
}
