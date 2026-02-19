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
	def := tool.Definition()
	if def.OfCodeInterpreter == nil {
		t.Fatal("expected code_interpreter tool definition")
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
	def := tool.Definition()
	if def.OfWebSearch == nil {
		t.Fatal("expected web_search tool definition")
	}
	out, err := tool.Call(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "built-in OpenAI tool") {
		t.Fatalf("unexpected output: %q", out)
	}
}
