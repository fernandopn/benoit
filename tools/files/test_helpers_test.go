package files

import (
	"errors"
	"io/fs"
	"os"
	"time"
)

type fakeFS struct {
	entries       map[string][]os.DirEntry
	readDirErr    map[string]error
	files         map[string][]byte
	readFileErr   map[string]error
	mkdirAllErr   map[string]error
	writeFileErr  map[string]error
	removeFileErr map[string]error
	cwd           string
	cwdErr        error
}

func (f fakeFS) ReadDir(name string) ([]os.DirEntry, error) {
	if err, ok := f.readDirErr[name]; ok {
		return nil, err
	}
	if entries, ok := f.entries[name]; ok {
		return entries, nil
	}
	if _, ok := f.files[name]; ok {
		return nil, errors.New("not a directory")
	}
	return nil, errors.New("path not found")
}

func (f fakeFS) ReadFile(name string) ([]byte, error) {
	if err, ok := f.readFileErr[name]; ok {
		return nil, err
	}
	if data, ok := f.files[name]; ok {
		return data, nil
	}
	if _, ok := f.entries[name]; ok {
		return nil, errors.New("is a directory")
	}
	return nil, errors.New("path not found")
}

func (f fakeFS) MkdirAll(name string) error {
	if err, ok := f.mkdirAllErr[name]; ok {
		return err
	}
	return nil
}

func (f fakeFS) WriteFile(name string, data []byte) error {
	if err, ok := f.writeFileErr[name]; ok {
		return err
	}
	if f.files == nil {
		return nil
	}
	cloned := make([]byte, len(data))
	copy(cloned, data)
	f.files[name] = cloned
	return nil
}

func (f fakeFS) RemoveFile(name string) error {
	if err, ok := f.removeFileErr[name]; ok {
		return err
	}
	if f.files != nil {
		delete(f.files, name)
	}
	return nil
}

func (f fakeFS) Getwd() (string, error) {
	if f.cwdErr != nil {
		return "", f.cwdErr
	}
	return f.cwd, nil
}

type fakeDirEntry struct {
	name string
	dir  bool
}

func (f fakeDirEntry) Name() string { return f.name }
func (f fakeDirEntry) IsDir() bool  { return f.dir }
func (f fakeDirEntry) Type() fs.FileMode {
	if f.dir {
		return fs.ModeDir
	}
	return 0
}

func (f fakeDirEntry) Info() (fs.FileInfo, error) {
	return fakeFileInfo{name: f.name, dir: f.dir}, nil
}

type fakeFileInfo struct {
	name string
	dir  bool
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode  { return fakeMode(f.dir) }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }

func fakeMode(dir bool) fs.FileMode {
	if dir {
		return fs.ModeDir
	}
	return 0
}
