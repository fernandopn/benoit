package files

import (
	"context"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// WriteFileTool writes text content to a file path.
type WriteFileTool struct {
	fs FileSystem
}

func NewWriteFileTool() *WriteFileTool {
	return NewWriteFileToolWithFS(osFS{})
}

func NewWriteFileToolWithFS(fs FileSystem) *WriteFileTool {
	return &WriteFileTool{fs: fs}
}

func (w *WriteFileTool) Name() string {
	return "write"
}

func (w *WriteFileTool) Definition() responses.ToolUnionParam {
	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name:        w.Name(),
			Description: openai.String("Write text content to a file path. Overwrites existing file content."),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					filePathArgName: map[string]any{
						"type":        "string",
						"description": "File path to write",
					},
					contentArgName: map[string]any{
						"type":        "string",
						"description": "Complete file content to write",
					},
				},
				"required":             []string{filePathArgName, contentArgName},
				"additionalProperties": false,
			},
			Strict: openai.Bool(true),
		},
	}
}

func (w *WriteFileTool) Call(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	if err := requireFileSystem(w.fs); err != nil {
		return toolError(err), nil
	}

	path, err := requiredFilePathArg(args)
	if err != nil {
		return toolError(err), nil
	}
	content, err := requiredRawStringArg(args, contentArgName)
	if err != nil {
		return toolError(err), nil
	}

	if err := w.fs.WriteFile(path, []byte(content)); err != nil {
		return toolError(err), nil
	}

	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}
