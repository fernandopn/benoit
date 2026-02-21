package files

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNewFileSystemPathValidatorRequiresPrefixes(t *testing.T) {
	if _, err := NewFileSystemPathValidator(nil); err == nil {
		t.Fatal("expected error for empty prefixes")
	}
	if _, err := NewFileSystemPathValidator([]string{"  "}); err == nil {
		t.Fatal("expected error for blank prefixes")
	}
}

func TestFileSystemPathValidatorPrimaryPrefix(t *testing.T) {
	v, err := NewFileSystemPathValidator([]string{"/root", "/tmp", "/root"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := v.PrimaryPrefix(); got != filepath.Clean("/root") {
		t.Fatalf("unexpected primary prefix: %q", got)
	}
}

func TestFileSystemPathValidatorAllowsMultiplePrefixes(t *testing.T) {
	v, err := NewFileSystemPathValidator([]string{"/root", "/tmp/work"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := v.Validate("/root/a.txt"); err != nil {
		t.Fatalf("expected /root path to be allowed, got error: %v", err)
	}
	if err := v.Validate("/tmp/work/b.txt"); err != nil {
		t.Fatalf("expected /tmp/work path to be allowed, got error: %v", err)
	}
}

func TestFileSystemPathValidatorRejectsOutsidePrefixes(t *testing.T) {
	v, err := NewFileSystemPathValidator([]string{"/root", "/tmp/work"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := v.Validate("/etc/passwd"); err == nil {
		t.Fatal("expected outside path to be rejected")
	}
}

func TestFileSystemPathValidatorResolve(t *testing.T) {
	v, err := NewFileSystemPathValidator([]string{"/root", "/tmp/work"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resolved, err := v.Resolve("file.txt", "/root")
	if err != nil {
		t.Fatalf("unexpected error resolving relative path: %v", err)
	}
	if want := filepath.Clean("/root/file.txt"); resolved != want {
		t.Fatalf("unexpected resolved path: got=%q want=%q", resolved, want)
	}

	if _, err := v.Resolve("../etc/passwd", "/root"); err == nil {
		t.Fatal("expected relative path escaping prefix to fail")
	}
}

func TestFileSystemPathValidatorResolveFromPrimaryPrefix(t *testing.T) {
	v, err := NewFileSystemPathValidator([]string{"/root", "/tmp/work"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resolved, err := v.Resolve("nested/file.txt", "")
	if err != nil {
		t.Fatalf("unexpected error resolving from primary prefix: %v", err)
	}
	if want := filepath.Clean("/root/nested/file.txt"); resolved != want {
		t.Fatalf("unexpected resolved path: got=%q want=%q", resolved, want)
	}
}

func TestHasPrefixPath(t *testing.T) {
	allowed, err := hasPrefixPath("/root", "/root/nested/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected path to be allowed")
	}

	allowed, err = hasPrefixPath("/root", "/etc/passwd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected path to be rejected")
	}
}

func TestRestrictedFSWithMultipleRoots(t *testing.T) {
	base := fakeFS{
		cwd: "/root",
		entries: map[string][]os.DirEntry{
			"/root/a":     {},
			"/tmp/work/b": {},
		},
		readDirErr: map[string]error{
			"/tmp/work/boom": errors.New("boom"),
		},
	}

	fs, err := NewRestrictedFSWithBaseAndRoots(base, []string{"/root", "/tmp/work"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := fs.ReadDir("/root/a"); err != nil {
		t.Fatalf("expected /root path to be allowed: %v", err)
	}
	if _, err := fs.ReadDir("/tmp/work/b"); err != nil {
		t.Fatalf("expected /tmp/work path to be allowed: %v", err)
	}
	if _, err := fs.ReadDir("/etc"); err == nil {
		t.Fatal("expected /etc to be rejected")
	}

	if _, err := fs.ReadDir("/tmp/work/boom"); err == nil {
		t.Fatal("expected base fs error to propagate")
	}
}
