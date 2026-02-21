package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileSystemPathValidator validates paths against one or more allowed prefixes.
type FileSystemPathValidator struct {
	prefixes []string
}

func NewFileSystemPathValidator(prefixes []string) (*FileSystemPathValidator, error) {
	normalized := make([]string, 0, len(prefixes))
	seen := make(map[string]struct{}, len(prefixes))
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		absPrefix, err := filepath.Abs(prefix)
		if err != nil {
			return nil, err
		}
		cleanPrefix := filepath.Clean(absPrefix)
		if _, exists := seen[cleanPrefix]; exists {
			continue
		}
		seen[cleanPrefix] = struct{}{}
		normalized = append(normalized, cleanPrefix)
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("at least one allowed root is required")
	}
	return &FileSystemPathValidator{prefixes: normalized}, nil
}

func (v *FileSystemPathValidator) PrimaryPrefix() string {
	if v == nil || len(v.prefixes) == 0 {
		return ""
	}
	return v.prefixes[0]
}

func (v *FileSystemPathValidator) Resolve(path string, relativeBase string) (string, error) {
	if v == nil || len(v.prefixes) == 0 {
		return "", fmt.Errorf("filesystem path validator is not configured")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errPathCannotBeEmpty
	}

	var absPath string
	if filepath.IsAbs(path) {
		absPath = filepath.Clean(path)
	} else {
		relativeBase = strings.TrimSpace(relativeBase)
		if relativeBase == "" {
			relativeBase = v.PrimaryPrefix()
		}
		if relativeBase == "" {
			return "", fmt.Errorf("relative path base is required")
		}
		absPath = filepath.Clean(filepath.Join(relativeBase, path))
	}

	if err := v.Validate(absPath); err != nil {
		return "", err
	}
	return absPath, nil
}

func (v *FileSystemPathValidator) Validate(path string) error {
	if v == nil || len(v.prefixes) == 0 {
		return fmt.Errorf("filesystem path validator is not configured")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return errPathCannotBeEmpty
	}

	absPath := path
	if !filepath.IsAbs(absPath) {
		resolved, err := filepath.Abs(absPath)
		if err != nil {
			return err
		}
		absPath = resolved
	}
	absPath = filepath.Clean(absPath)

	for _, prefix := range v.prefixes {
		allowed, err := hasPrefixPath(prefix, absPath)
		if err != nil {
			continue
		}
		if allowed {
			return nil
		}
	}

	if len(v.prefixes) == 1 {
		return fmt.Errorf("path outside allowed root: %s", v.prefixes[0])
	}
	return fmt.Errorf("path outside allowed roots: %s", strings.Join(v.prefixes, ", "))
}

func hasPrefixPath(prefix string, path string) (bool, error) {
	rel, err := filepath.Rel(prefix, path)
	if err != nil {
		return false, err
	}
	if rel == "." {
		return true, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false, nil
	}
	return true, nil
}
