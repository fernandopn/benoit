package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NewChrootFS returns a filesystem that exposes root as virtual "/".
func NewChrootFS(root string) (FileSystem, error) {
	return NewChrootFSWithBaseAndRoots(osFS{}, root, []string{root})
}

// NewChrootFSWithBase returns a filesystem that exposes root as virtual "/".
func NewChrootFSWithBase(base FileSystem, root string) (FileSystem, error) {
	return NewChrootFSWithBaseAndRoots(base, root, []string{root})
}

// NewChrootFSWithBaseAndRoots returns a filesystem with chroot-like path mapping and prefix validation.
func NewChrootFSWithBaseAndRoots(base FileSystem, root string, roots []string) (FileSystem, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("root cannot be empty")
	}
	if base == nil {
		base = osFS{}
	}

	validator, err := NewFileSystemPathValidator(roots)
	if err != nil {
		return nil, err
	}

	hostRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	hostRoot = filepath.Clean(hostRoot)
	if err := validator.Validate(hostRoot); err != nil {
		return nil, err
	}

	return chrootFS{base: base, hostRoot: hostRoot, validator: validator}, nil
}

type chrootFS struct {
	base      FileSystem
	hostRoot  string
	validator *FileSystemPathValidator
}

func (c chrootFS) ReadDir(name string) ([]os.DirEntry, error) {
	hostPath, err := c.resolve(name)
	if err != nil {
		return nil, err
	}
	return c.base.ReadDir(hostPath)
}

func (c chrootFS) ReadFile(name string) ([]byte, error) {
	hostPath, err := c.resolve(name)
	if err != nil {
		return nil, err
	}
	return c.base.ReadFile(hostPath)
}

func (c chrootFS) WriteFile(name string, data []byte) error {
	hostPath, err := c.resolve(name)
	if err != nil {
		return err
	}
	return c.base.WriteFile(hostPath, data)
}

func (c chrootFS) RemoveFile(name string) error {
	hostPath, err := c.resolve(name)
	if err != nil {
		return err
	}
	return c.base.RemoveFile(hostPath)
}

func (c chrootFS) Getwd() (string, error) {
	return string(filepath.Separator), nil
}

func (c chrootFS) resolve(path string) (string, error) {
	if c.validator == nil {
		return "", fmt.Errorf("filesystem path validator is not configured")
	}

	virtualPath, err := normalizeVirtualPath(path)
	if err != nil {
		return "", err
	}

	hostPath := c.hostPath(virtualPath)
	if err := c.validator.Validate(hostPath); err != nil {
		return "", err
	}
	return hostPath, nil
}

func normalizeVirtualPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errPathCannotBeEmpty
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Clean(filepath.Join(string(filepath.Separator), path)), nil
}

func (c chrootFS) hostPath(virtualPath string) string {
	if virtualPath == string(filepath.Separator) {
		return c.hostRoot
	}
	rel := strings.TrimPrefix(virtualPath, string(filepath.Separator))
	if rel == "" {
		return c.hostRoot
	}
	return filepath.Clean(filepath.Join(c.hostRoot, rel))
}
