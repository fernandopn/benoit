package tools

import (
	"context"
	"strings"
	"testing"
)

func TestOpenAICodeInterpreterTool(t *testing.T) {
	tool := NewOpenAICodeInterpreterTool()
	if got := tool.Name(); got != "code_interpreter" {
		t.Fatalf("unexpected name: %q", got)
	}
	schema := tool.Schema()
	if schema.Name != "code_interpreter" {
		t.Fatalf("unexpected schema name: %q", schema.Name)
	}
	if schema.Kind != ToolKindHostedCodeInterpreter {
		t.Fatalf("unexpected kind: %q", schema.Kind)
	}
	out, err := tool.Call(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "built-in OpenAI tool") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestOpenAIWebSearchTool(t *testing.T) {
	tool := NewOpenAIWebSearchTool()
	if got := tool.Name(); got != "web_search" {
		t.Fatalf("unexpected name: %q", got)
	}
	schema := tool.Schema()
	if schema.Name != "web_search" {
		t.Fatalf("unexpected schema name: %q", schema.Name)
	}
	if schema.Kind != ToolKindHostedWebSearch {
		t.Fatalf("unexpected kind: %q", schema.Kind)
	}
	out, err := tool.Call(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "built-in OpenAI tool") {
		t.Fatalf("unexpected output: %q", out)
	}
}
