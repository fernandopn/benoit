package files

import (
	"context"
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
					pathArgName: map[string]any{
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
	if err := requireFileSystem(l.fs); err != nil {
		return toolError(err), nil
	}

	path, err := optionalPathArg(args)
	if err != nil {
		return toolError(err), nil
	}

	entries, err := l.fs.ReadDir(path)
	if err != nil {
		return toolError(err), nil
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
