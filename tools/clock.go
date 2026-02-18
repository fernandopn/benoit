package tools

import (
	"context"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// ClockTool provides the current time.
type ClockTool struct{}

func NewClockTool() *ClockTool {
	return &ClockTool{}
}

func (c *ClockTool) Name() string {
	return "get_time"
}

func (c *ClockTool) Definition() responses.ToolUnionParam {
	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name:        c.Name(),
			Description: openai.String("Return the current time as a string"),
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
			Strict: openai.Bool(true),
		},
	}
}

func (c *ClockTool) Call(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	_ = args
	return time.Now().Format(time.RFC3339), nil
}
