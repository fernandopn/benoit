package files

import (
	"fmt"
	"strconv"
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
	value, err := asPath(raw)
	if err != nil {
		return "", err
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
	value, err := asPath(raw)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errPathCannotBeEmpty
	}
	return value, nil
}

func requiredFilePathArg(args map[string]any) (string, error) {
	if args == nil {
		return "", errFilePathRequired
	}
	raw, ok := args[filePathArgName]
	if !ok {
		return "", errFilePathRequired
	}
	value, ok := raw.(string)
	if !ok {
		return "", errFilePathMustBeString
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errFilePathCannotBeEmpty
	}
	return value, nil
}

func requiredRawStringArg(args map[string]any, key string) (string, error) {
	if args == nil {
		return "", fmt.Errorf("missing required argument: %s", key)
	}
	raw, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument: %s", key)
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return value, nil
}

func optionalBoolArg(args map[string]any, key string, defaultValue bool) (bool, error) {
	if args == nil {
		return defaultValue, nil
	}
	raw, ok := args[key]
	if !ok {
		return defaultValue, nil
	}
	switch value := raw.(type) {
	case bool:
		return value, nil
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return false, fmt.Errorf("%s must be a boolean", key)
		}
		return parsed, nil
	default:
		return false, fmt.Errorf("%s must be a boolean", key)
	}
}

func optionalPositiveIntArg(args map[string]any, key string, defaultValue int) (int, error) {
	if args == nil {
		return defaultValue, nil
	}
	raw, ok := args[key]
	if !ok {
		return defaultValue, nil
	}
	parsed, err := asInt(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", key)
	}
	return parsed, nil
}

func requiredStringArg(args map[string]any, key string) (string, error) {
	if args == nil {
		return "", fmt.Errorf("missing required argument: %s", key)
	}
	raw, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument: %s", key)
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s cannot be empty", key)
	}
	return value, nil
}

func asInt(raw any) (int, error) {
	switch value := raw.(type) {
	case int:
		return value, nil
	case int8:
		return int(value), nil
	case int16:
		return int(value), nil
	case int32:
		return int(value), nil
	case int64:
		return int(value), nil
	case float64:
		if float64(int(value)) != value {
			return 0, fmt.Errorf("not an integer")
		}
		return int(value), nil
	case float32:
		if float32(int(value)) != value {
			return 0, fmt.Errorf("not an integer")
		}
		return int(value), nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, fmt.Errorf("empty")
		}
		parsed, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported type")
	}
}

func asPath(raw any) (string, error) {
	value, ok := raw.(string)
	if !ok {
		return "", errPathMustBeString
	}
	return value, nil
}

func requireFileSystem(fs FileSystem) error {
	if fs == nil {
		return errFilesystemNotConfigured
	}
	return nil
}
