package tools

import (
	"context"

	"github.com/openai/openai-go/v3/responses"
)

// OpenAICodeInterpreterTool enables OpenAI's built-in code interpreter tool.
type OpenAICodeInterpreterTool struct{}

func NewOpenAICodeInterpreterTool() *OpenAICodeInterpreterTool {
	return &OpenAICodeInterpreterTool{}
}

func (t *OpenAICodeInterpreterTool) Name() string {
	return "code_interpreter"
}

func (t *OpenAICodeInterpreterTool) Definition() responses.ToolUnionParam {
	return responses.ToolParamOfCodeInterpreter(responses.ToolCodeInterpreterContainerCodeInterpreterContainerAutoParam{})
}

func (t *OpenAICodeInterpreterTool) Call(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	_ = args
	return "error: code_interpreter is a built-in OpenAI tool and cannot be called directly", nil
}

// OpenAIWebSearchTool enables OpenAI's built-in web search tool.
type OpenAIWebSearchTool struct{}

func NewOpenAIWebSearchTool() *OpenAIWebSearchTool {
	return &OpenAIWebSearchTool{}
}

func (t *OpenAIWebSearchTool) Name() string {
	return "web_search"
}

func (t *OpenAIWebSearchTool) Definition() responses.ToolUnionParam {
	return responses.ToolParamOfWebSearch(responses.WebSearchToolTypeWebSearch)
}

func (t *OpenAIWebSearchTool) Call(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	_ = args
	return "error: web_search is a built-in OpenAI tool and cannot be called directly", nil
}
