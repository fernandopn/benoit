package files

import (
	"context"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

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
	if err := requireFileSystem(c.fs); err != nil {
		return toolError(err), nil
	}

	dir, err := c.fs.Getwd()
	if err != nil {
		return toolError(err), nil
	}

	return dir, nil
}
