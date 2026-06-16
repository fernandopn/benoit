package tools

import (
	"context"
	"time"
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

func (c *ClockTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        c.Name(),
		Description: "Return the current time as a string",
		Parameters: MustParameters(map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		}),
		Kind:   ToolKindFunction,
		Strict: true,
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
