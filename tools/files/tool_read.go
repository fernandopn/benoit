package files

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/fernandopn/benoit/tools"
)

const defaultReadLimit = 2000
const maxReadLineLength = 2000

// ReadFileTool reads file or directory content.
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
	return "read"
}

func (r *ReadFileTool) Schema() tools.ToolSchema {
	return tools.ToolSchema{
		Name:        r.Name(),
		Description: "Read a file or directory from the filesystem sandbox. Returns numbered lines for files and plain entries for directories.",
		Parameters: tools.MustParameters(map[string]any{
			"type": "object",
			"properties": map[string]any{
				filePathArgName: map[string]any{
					"type":        "string",
					"description": "Sandbox file or directory path. Use / as the sandbox root.",
				},
				offsetArgName: map[string]any{
					"type":        "integer",
					"description": "1-based starting line for file reads",
				},
				limitArgName: map[string]any{
					"type":        "integer",
					"description": "Maximum number of lines to return (default 2000)",
				},
			},
			"required":             []string{filePathArgName},
			"additionalProperties": false,
		}),
		Kind:   tools.ToolKindFunction,
		Strict: false,
	}
}

func (r *ReadFileTool) Call(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	if err := requireFileSystem(r.fs); err != nil {
		return toolError(err), nil
	}

	path, err := requiredFilePathArg(args)
	if err != nil {
		return toolError(err), nil
	}
	offset, err := optionalPositiveIntArg(args, offsetArgName, 1)
	if err != nil {
		return toolError(err), nil
	}
	limit, err := optionalPositiveIntArg(args, limitArgName, defaultReadLimit)
	if err != nil {
		return toolError(err), nil
	}

	if entries, dirErr := r.fs.ReadDir(path); dirErr == nil {
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

	data, err := r.fs.ReadFile(path)
	if err != nil {
		return toolError(err), nil
	}
	return formatReadContent(string(data), offset, limit), nil
}

func formatReadContent(content string, offset int, limit int) string {
	lines := strings.Split(content, "\n")
	if offset > len(lines) {
		return ""
	}
	start := offset - 1
	end := start + limit
	if end > len(lines) {
		end = len(lines)
	}
	formatted := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		line := lines[i]
		if len(line) > maxReadLineLength {
			line = line[:maxReadLineLength]
		}
		formatted = append(formatted, fmt.Sprintf("%d: %s", i+1, line))
	}
	return strings.Join(formatted, "\n")
}
