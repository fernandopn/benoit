package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// ListFilesTool lists entries in a directory.
type ListFilesTool struct {
	fs FileSystem
}

func NewListFilesTool() *ListFilesTool {
	return NewListFilesToolWithFS(osFS{})
}

func NewListFilesToolWithFS(fs FileSystem) *ListFilesTool {
	return &ListFilesTool{fs: fs}
}

func (l *ListFilesTool) Name() string {
	return "list_files"
}

func (l *ListFilesTool) Definition() responses.ToolUnionParam {
	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name:        l.Name(),
			Description: openai.String("List files and directories in a given path. Returns newline-separated names; directories end with /. If no path is provided, the current directory is listed."),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Directory path to list (optional)",
					},
				},
				"additionalProperties": false,
			},
			Strict: openai.Bool(false),
		},
	}
}

func (l *ListFilesTool) Call(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	if l.fs == nil {
		return "error: filesystem not configured", nil
	}
	path := "."
	if raw, ok := args["path"]; ok {
		value, ok := raw.(string)
		if !ok {
			return "error: path must be a string", nil
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return "error: path cannot be empty", nil
		}
		path = value
	}
	entries, err := l.fs.ReadDir(path)
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, "\n"), nil
}

// CurrentDirectoryTool returns the current working directory.
type CurrentDirectoryTool struct {
	fs FileSystem
}

func NewCurrentDirectoryTool() *CurrentDirectoryTool {
	return NewCurrentDirectoryToolWithFS(osFS{})
}

func NewCurrentDirectoryToolWithFS(fs FileSystem) *CurrentDirectoryTool {
	return &CurrentDirectoryTool{fs: fs}
}

func (c *CurrentDirectoryTool) Name() string {
	return "get_current_directory"
}

func (c *CurrentDirectoryTool) Definition() responses.ToolUnionParam {
	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name:        c.Name(),
			Description: openai.String("Return the current working directory as a string."),
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
			Strict: openai.Bool(true),
		},
	}
}

func (c *CurrentDirectoryTool) Call(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	_ = args
	if c.fs == nil {
		return "error: filesystem not configured", nil
	}
	dir, err := c.fs.Getwd()
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	return dir, nil
}

// ReadFileTool reads a file's contents.
type ReadFileTool struct {
	fs FileSystem
}

func NewReadFileTool() *ReadFileTool {
	return NewReadFileToolWithFS(osFS{})
}

func NewReadFileToolWithFS(fs FileSystem) *ReadFileTool {
	return &ReadFileTool{fs: fs}
}

func (r *ReadFileTool) Name() string {
	return "read_file"
}

func (r *ReadFileTool) Definition() responses.ToolUnionParam {
	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name:        r.Name(),
			Description: openai.String("Read a file and return its contents as text."),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "File path to read",
					},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			},
			Strict: openai.Bool(true),
		},
	}
}

func (r *ReadFileTool) Call(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	if r.fs == nil {
		return "error: filesystem not configured", nil
	}
	raw, ok := args["path"]
	if !ok {
		return "error: missing required argument: path", nil
	}
	path, ok := raw.(string)
	if !ok {
		return "error: path must be a string", nil
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "error: path cannot be empty", nil
	}
	data, err := r.fs.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	return string(data), nil
}
