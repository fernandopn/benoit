package files

import (
	"fmt"
	"os"
	"strings"
)

// NewRestrictedFS returns a filesystem restricted to the provided root.
func NewRestrictedFS(root string) (FileSystem, error) {
	return NewRestrictedFSWithBase(osFS{}, root)
}

// NewRestrictedFSWithBase returns a filesystem restricted to the provided root.
func NewRestrictedFSWithBase(base FileSystem, root string) (FileSystem, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("root cannot be empty")
	}
	return NewRestrictedFSWithBaseAndRoots(base, []string{root})
}

// NewRestrictedFSWithRoots returns a filesystem restricted to the provided roots.
func NewRestrictedFSWithRoots(roots []string) (FileSystem, error) {
	return NewRestrictedFSWithBaseAndRoots(osFS{}, roots)
}

// NewRestrictedFSWithBaseAndRoots returns a filesystem restricted to one or more roots.
func NewRestrictedFSWithBaseAndRoots(base FileSystem, roots []string) (FileSystem, error) {
	if base == nil {
		base = osFS{}
	}
	validator, err := NewFileSystemPathValidator(roots)
	if err != nil {
		return nil, err
	}
	return restrictedFS{base: base, root: validator.PrimaryPrefix(), validator: validator}, nil
}

type restrictedFS struct {
	base      FileSystem
	root      string
	validator *FileSystemPathValidator
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

func (r restrictedFS) MkdirAll(name string) error {
	path, err := r.resolve(name)
	if err != nil {
		return err
	}
	return r.base.MkdirAll(path)
}

func (r restrictedFS) WriteFile(name string, data []byte) error {
	path, err := r.resolve(name)
	if err != nil {
		return err
	}
	return r.base.WriteFile(path, data)
}

func (r restrictedFS) RemoveFile(name string) error {
	path, err := r.resolve(name)
	if err != nil {
		return err
	}
	return r.base.RemoveFile(path)
}

func (r restrictedFS) Getwd() (string, error) {
	if r.root == "" {
		return r.base.Getwd()
	}
	return r.root, nil
}

func (r restrictedFS) resolve(name string) (string, error) {
	if r.validator == nil {
		return "", fmt.Errorf("filesystem path validator is not configured")
	}
	return r.validator.Resolve(name, r.root)
}
