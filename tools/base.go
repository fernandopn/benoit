package tools

import (
	"context"

	"github.com/openai/openai-go/v3/responses"
)

// Tool defines a callable tool that can be attached to model requests.
type Tool interface {
	Name() string
	Definition() responses.ToolUnionParam
	Call(ctx context.Context, args map[string]any) (string, error)
}
