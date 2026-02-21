package files

import (
	"fmt"
	"strings"
)

func toolError(err error) string {
	if err == nil {
		return "error"
	}
	return fmt.Sprintf("error: %v", err)
}

func optionalPathArg(args map[string]any) (string, error) {
	path := "."
	if args == nil {
		return path, nil
	}
	raw, ok := args[pathArgName]
	if !ok {
		return path, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", errPathMustBeString
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errPathCannotBeEmpty
	}
	return value, nil
}

func requiredPathArg(args map[string]any) (string, error) {
	if args == nil {
		return "", errPathRequired
	}
	raw, ok := args[pathArgName]
	if !ok {
		return "", errPathRequired
	}
	value, ok := raw.(string)
	if !ok {
		return "", errPathMustBeString
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errPathCannotBeEmpty
	}
	return value, nil
}

func requireFileSystem(fs FileSystem) error {
	if fs == nil {
		return errFilesystemNotConfigured
	}
	return nil
}
