package tools

import (
	"context"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// ClockTool provides the current time.
type ClockTool struct {
	now func() time.Time
}

func NewClockTool() *ClockTool {
	return NewClockToolWithNow(time.Now)
}

func NewClockToolWithNow(now func() time.Time) *ClockTool {
	return &ClockTool{now: now}
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
	if c.now == nil {
		return "error: clock not configured", nil
	}
	return c.now().Format(time.RFC3339), nil
}
