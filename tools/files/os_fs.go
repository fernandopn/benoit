package files

import (
	"os"
	"path/filepath"
)

type osFS struct{}

func (osFS) ReadDir(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

func (osFS) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (osFS) MkdirAll(name string) error {
	return os.MkdirAll(name, 0o755)
}

func (osFS) WriteFile(name string, data []byte) error {
	dir := filepath.Dir(name)
	if dir != "" && dir != "." {
		if err := (osFS{}).MkdirAll(dir); err != nil {
			return err
		}
	}
	return os.WriteFile(name, data, 0o644)
}

func (osFS) RemoveFile(name string) error {
	return os.Remove(name)
}

func (osFS) Getwd() (string, error) {
	return os.Getwd()
}
