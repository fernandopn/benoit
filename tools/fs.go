package tools

import "os"

// FileSystem abstracts filesystem access for tools.
type FileSystem interface {
	ReadDir(name string) ([]os.DirEntry, error)
	Getwd() (string, error)
}

type osFS struct{}

func (osFS) ReadDir(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

func (osFS) Getwd() (string, error) {
	return os.Getwd()
}
