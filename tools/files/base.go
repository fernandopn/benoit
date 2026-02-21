package files

import (
	"errors"
	"os"
)

const (
	pathArgName      = "path"
	patternArgName   = "pattern"
	includeArgName   = "include"
	filePathArgName  = "filePath"
	contentArgName   = "content"
	patchTextArgName = "patchText"
	offsetArgName    = "offset"
	limitArgName     = "limit"
)

var (
	errFilesystemNotConfigured = errors.New("filesystem not configured")
	errPathMustBeString        = errors.New("path must be a string")
	errPathCannotBeEmpty       = errors.New("path cannot be empty")
	errPathRequired            = errors.New("missing required argument: path")
	errFilePathMustBeString    = errors.New("filePath must be a string")
	errFilePathCannotBeEmpty   = errors.New("filePath cannot be empty")
	errFilePathRequired        = errors.New("missing required argument: filePath")
	errPatchTextRequired       = errors.New("missing required argument: patchText")
)

// FileSystem abstracts filesystem access for tools.
type FileSystem interface {
	ReadDir(name string) ([]os.DirEntry, error)
	ReadFile(name string) ([]byte, error)
	MkdirAll(name string) error
	WriteFile(name string, data []byte) error
	RemoveFile(name string) error
	Getwd() (string, error)
}
