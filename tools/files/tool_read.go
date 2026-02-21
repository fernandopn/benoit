package files

import (
	"context"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

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
					pathArgName: map[string]any{
						"type":        "string",
						"description": "File path to read",
					},
				},
				"required":             []string{pathArgName},
				"additionalProperties": false,
			},
			Strict: openai.Bool(true),
		},
	}
}

func (r *ReadFileTool) Call(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	if err := requireFileSystem(r.fs); err != nil {
		return toolError(err), nil
	}

	path, err := requiredPathArg(args)
	if err != nil {
		return toolError(err), nil
	}

	data, err := r.fs.ReadFile(path)
	if err != nil {
		return toolError(err), nil
	}

	return string(data), nil
}
