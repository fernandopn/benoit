package files

import (
	"errors"
	"os"
)

const pathArgName = "path"

var (
	errFilesystemNotConfigured = errors.New("filesystem not configured")
	errPathMustBeString        = errors.New("path must be a string")
	errPathCannotBeEmpty       = errors.New("path cannot be empty")
	errPathRequired            = errors.New("missing required argument: path")
)

// FileSystem abstracts filesystem access for tools.
type FileSystem interface {
	ReadDir(name string) ([]os.DirEntry, error)
	ReadFile(name string) ([]byte, error)
	Getwd() (string, error)
}
