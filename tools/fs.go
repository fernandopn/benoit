package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileSystem abstracts filesystem access for tools.
type FileSystem interface {
	ReadDir(name string) ([]os.DirEntry, error)
	ReadFile(name string) ([]byte, error)
	Getwd() (string, error)
}

type osFS struct{}

func (osFS) ReadDir(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

func (osFS) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (osFS) Getwd() (string, error) {
	return os.Getwd()
}

// NewRestrictedFS returns a filesystem restricted to the provided root.
// If root is empty, the current working directory is used.
func NewRestrictedFS(root string) (FileSystem, error) {
	return NewRestrictedFSWithBase(osFS{}, root)
}

// NewRestrictedFSWithBase returns a filesystem restricted to the provided root.
func NewRestrictedFSWithBase(base FileSystem, root string) (FileSystem, error) {
	if base == nil {
		base = osFS{}
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("root cannot be empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return restrictedFS{base: base, root: filepath.Clean(abs)}, nil
}

type restrictedFS struct {
	base FileSystem
	root string
}

func (r restrictedFS) ReadDir(name string) ([]os.DirEntry, error) {
	path, err := r.resolve(name)
	if err != nil {
		return nil, err
	}
	return r.base.ReadDir(path)
}

func (r restrictedFS) ReadFile(name string) ([]byte, error) {
	path, err := r.resolve(name)
	if err != nil {
		return nil, err
	}
	return r.base.ReadFile(path)
}

func (r restrictedFS) Getwd() (string, error) {
	if r.root == "" {
		return r.base.Getwd()
	}
	return r.root, nil
}

func (r restrictedFS) resolve(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	var abs string
	if filepath.IsAbs(name) {
		abs = filepath.Clean(name)
	} else {
		abs = filepath.Clean(filepath.Join(r.root, name))
	}
	rel, err := filepath.Rel(r.root, abs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path outside allowed root: %s", r.root)
	}
	return abs, nil
}
