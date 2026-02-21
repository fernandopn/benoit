package bubbletea

import (
	"strings"
	"testing"

	"github.com/fernandopn/benoit/providers"
)

func TestApplyStreamMessagesMergesToolResultIntoCallBlock(t *testing.T) {
	m := newTestModel()
	meta := map[string]string{"tool": "glob", "call_id": "call-1"}

	m.applyStreamMessages([]providers.Msg{{
		Type:     providers.MsgTypeToolCall,
		Value:    `{"path":"./src","recursive":false}`,
		Metadata: meta,
	}})

	initialToolCount := countToolBlocks(m.blocks)
	if initialToolCount != 1 {
		t.Fatalf("expected one tool block after tool call, got %d", initialToolCount)
	}

	m.applyStreamMessages([]providers.Msg{{
		Type:     providers.MsgTypeToolResult,
		Value:    "src/a.go\nsrc/b.go",
		Metadata: meta,
	}})

	finalToolCount := countToolBlocks(m.blocks)
	if finalToolCount != 1 {
		t.Fatalf("expected tool result to update existing tool block, got %d tool blocks", finalToolCount)
	}

	toolBlock := latestToolBlock(m.blocks)
	if toolBlock == nil {
		t.Fatalf("expected a tool block to exist")
	}
	if !strings.Contains(toolBlock.ToolArgs, `"path":"./src"`) {
		t.Fatalf("expected tool args to be retained in tool block")
	}
	if !strings.Contains(toolBlock.ToolResult, "src/a.go") {
		t.Fatalf("expected tool result to be appended to existing tool block")
	}
	if toolBlock.ToolState != toolExecutionDone {
		t.Fatalf("expected tool block to be marked done, got state %d", toolBlock.ToolState)
	}
}

func TestApplyStreamMessagesMarksPendingToolError(t *testing.T) {
	m := newTestModel()
	meta := map[string]string{"tool": "glob", "call_id": "call-err"}

	m.applyStreamMessages([]providers.Msg{{
		Type:     providers.MsgTypeToolCall,
		Value:    `{"path":"./src"}`,
		Metadata: meta,
	}})
	if !m.hasPendingToolResults() {
		t.Fatalf("expected pending tool results after tool call")
	}

	m.applyStreamMessages([]providers.Msg{{
		Type:  providers.MsgTypeError,
		Value: "tool execution failed",
	}})

	if m.hasPendingToolResults() {
		t.Fatalf("expected pending tool results to be cleared after error")
	}

	toolBlock := latestToolBlock(m.blocks)
	if toolBlock == nil {
		t.Fatalf("expected tool block to remain after error")
	}
	if toolBlock.ToolState != toolExecutionError {
		t.Fatalf("expected tool block to be marked as error, got state %d", toolBlock.ToolState)
	}
	if !strings.Contains(toolBlock.ToolResult, "tool execution failed") {
		t.Fatalf("expected tool error text to be visible in tool block result")
	}
}

func countToolBlocks(blocks []block) int {
	count := 0
	for i := range blocks {
		if blocks[i].Kind == blockToolWidget {
			count++
		}
	}
	return count
}

func latestToolBlock(blocks []block) *block {
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Kind == blockToolWidget {
			return &blocks[i]
		}
	}
	return nil
}
