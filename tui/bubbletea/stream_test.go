package bubbletea

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
	if !toolBlock.ToolResultReceived {
		t.Fatalf("expected tool block to mark tool result as received")
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

func TestStartStreamReturnsStartedMsg(t *testing.T) {
	canceled := false
	cancel := func() {
		canceled = true
	}

	ch := make(chan providers.Msg)
	msg := startStream(context.Background(), func(context.Context, string) (<-chan providers.Msg, context.CancelFunc, error) {
		return ch, cancel, nil
	}, "hello", 2)

	gotMsg := msg()
	got, ok := gotMsg.(streamStartedMsg)
	if !ok {
		t.Fatalf("expected streamStartedMsg, got %T", gotMsg)
	}
	if got.Seq != 2 {
		t.Fatalf("unexpected seq: %d", got.Seq)
	}
	if got.Ch != ch {
		t.Fatalf("expected returned channel to be reused")
	}
	if got.Cancel == nil {
		t.Fatal("expected cancel callback")
	}
	got.Cancel()
	if !canceled {
		t.Fatal("expected cancel callback to be invoked")
	}
}

func TestStartStreamPropagatesErrors(t *testing.T) {
	expectedErr := errors.New("boom")
	msg := startStream(context.Background(), func(context.Context, string) (<-chan providers.Msg, context.CancelFunc, error) {
		return nil, nil, expectedErr
	}, "hello", 3)

	gotMsg := msg()
	got, ok := gotMsg.(streamStartFailedMsg)
	if !ok {
		t.Fatalf("expected streamStartFailedMsg, got %T", gotMsg)
	}
	if got.Seq != 3 {
		t.Fatalf("unexpected seq: %d", got.Seq)
	}
	if !errors.Is(got.Err, expectedErr) {
		t.Fatalf("expected error %q, got %q", expectedErr, got.Err)
	}

	if got.Cancel != nil {
		got.Cancel()
	}
}

func TestStartStreamRejectsNilChannel(t *testing.T) {
	canceled := false
	msg := startStream(context.Background(), func(context.Context, string) (<-chan providers.Msg, context.CancelFunc, error) {
		return nil, func() {
			canceled = true
		}, nil
	}, "hello", 4)

	gotMsg := msg()
	got, ok := gotMsg.(streamStartFailedMsg)
	if !ok {
		t.Fatalf("expected streamStartFailedMsg, got %T", gotMsg)
	}
	if got.Seq != 4 {
		t.Fatalf("unexpected seq: %d", got.Seq)
	}
	if got.Err == nil {
		t.Fatal("expected error for nil channel")
	}
	if !strings.Contains(got.Err.Error(), "nil channel") {
		t.Fatalf("unexpected error message: %q", got.Err)
	}

	if !canceled {
		t.Fatal("expected cancel callback to run")
	}
}

func TestReadStreamChunk(t *testing.T) {
	t.Run("closed channel", func(t *testing.T) {
		ch := make(chan providers.Msg)
		close(ch)

		gotMsg := readStreamChunk(ch, 25*time.Millisecond, 4, 1)()
		got, ok := gotMsg.(streamChunkMsg)
		if !ok {
			t.Fatalf("expected streamChunkMsg, got %T", gotMsg)
		}
		if !got.Done {
			t.Fatal("expected closed channel to mark done")
		}
		if len(got.Msgs) != 0 {
			t.Fatalf("expected no messages, got %d", len(got.Msgs))
		}
	})

	t.Run("respects max message count", func(t *testing.T) {
		ch := make(chan providers.Msg, 3)
		ch <- providers.Msg{Type: providers.MsgTypeChatDelta, Value: "1"}
		ch <- providers.Msg{Type: providers.MsgTypeChatDelta, Value: "2"}

		gotMsg := readStreamChunk(ch, 25*time.Millisecond, 1, 2)()
		got, ok := gotMsg.(streamChunkMsg)
		if !ok {
			t.Fatalf("expected streamChunkMsg, got %T", gotMsg)
		}
		if got.Done {
			t.Fatal("expected max message cutoff to continue streaming")
		}
		if len(got.Msgs) != 1 {
			t.Fatalf("expected 1 message at limit, got %d", len(got.Msgs))
		}
		if got.Msgs[0].Value != "1" {
			t.Fatalf("expected first chunk, got %q", got.Msgs[0].Value)
		}
	})

	t.Run("returns on timer", func(t *testing.T) {
		ch := make(chan providers.Msg, 1)
		ch <- providers.Msg{Type: providers.MsgTypeChatDelta, Value: "first"}

		gotMsg := readStreamChunk(ch, 5*time.Millisecond, 3, 3)()
		got, ok := gotMsg.(streamChunkMsg)
		if !ok {
			t.Fatalf("expected streamChunkMsg, got %T", gotMsg)
		}
		if got.Done {
			t.Fatal("expected timer boundary to keep streaming")
		}
		if len(got.Msgs) != 1 {
			t.Fatalf("expected one message, got %d", len(got.Msgs))
		}
	})
}

func TestAppendToolCallCreatesToolBlockForUnknownCallID(t *testing.T) {
	m := newTestModel()
	m.appendToolCall("{\"path\":\"/\"}", nil)

	if countToolBlocks(m.blocks) != 1 {
		t.Fatalf("expected one tool block, got %d", countToolBlocks(m.blocks))
	}
	toolBlock := latestToolBlock(m.blocks)
	if toolBlock == nil {
		t.Fatal("expected tool block")
	}
	if toolBlock.ToolState != toolExecutionPending {
		t.Fatalf("expected pending tool state, got %d", toolBlock.ToolState)
	}
}

func TestAppendToolCallAppendsToMatchingCallBlock(t *testing.T) {
	m := newTestModel()
	meta := map[string]string{"call_id": "call-1", "tool": "glob"}

	m.appendToolCall(`{"path":"/src"}`, meta)
	m.appendToolCall(`{"depth":2}`, meta)

	if countToolBlocks(m.blocks) != 1 {
		t.Fatalf("expected one tool block for matching call, got %d", countToolBlocks(m.blocks))
	}
	toolBlock := latestToolBlock(m.blocks)
	if !strings.Contains(toolBlock.ToolArgs, `{"path":"/src"}{"depth":2}`) {
		t.Fatalf("expected tool args to append, got %q", toolBlock.ToolArgs)
	}
}

func TestAppendToolResultForUnknownCallIDCreatesNewBlock(t *testing.T) {
	m := newTestModel()
	m.appendToolCall(`{"path":"/src"}`, map[string]string{"call_id": "call-known", "tool": "glob"})
	m.appendToolResult("cached", map[string]string{"call_id": "call-unknown", "tool": "glob"})

	if countToolBlocks(m.blocks) != 2 {
		t.Fatalf("expected second tool block for unknown call id, got %d", countToolBlocks(m.blocks))
	}
	toolBlock := latestToolBlock(m.blocks)
	if toolBlock.ToolState != toolExecutionDone {
		t.Fatalf("expected new tool block to be done, got %d", toolBlock.ToolState)
	}
	if !toolBlock.ToolResultReceived {
		t.Fatalf("expected new tool block to mark tool result as received")
	}
}

func TestApplyStreamMessagesEmptyToolResultStillCompletesBlock(t *testing.T) {
	m := newTestModel()
	meta := map[string]string{"tool": "glob", "call_id": "call-empty"}

	m.applyStreamMessages([]providers.Msg{{
		Type:     providers.MsgTypeToolCall,
		Value:    `{"path":"/","pattern":"**/*"}`,
		Metadata: meta,
	}})

	if !m.hasPendingToolResults() {
		t.Fatalf("expected pending tool while waiting for result")
	}

	m.applyStreamMessages([]providers.Msg{{
		Type:     providers.MsgTypeToolResult,
		Value:    "",
		Metadata: meta,
	}})

	if m.hasPendingToolResults() {
		t.Fatalf("expected pending tool state to clear after empty result")
	}

	toolBlock := latestToolBlock(m.blocks)
	if toolBlock == nil {
		t.Fatalf("expected tool block after empty result")
	}
	if toolBlock.ToolState != toolExecutionDone {
		t.Fatalf("expected empty result to mark tool as done, got %d", toolBlock.ToolState)
	}
	if !toolBlock.ToolResultReceived {
		t.Fatalf("expected empty result to mark tool result as received")
	}
}

func TestAppendToolCallDoesNotRevertCompletedEmptyResultToPending(t *testing.T) {
	m := newTestModel()
	meta := map[string]string{"tool": "glob", "call_id": "call-reappend"}

	m.appendToolCall(`{"path":"/"}`, meta)
	m.appendToolResult("", meta)
	m.appendToolCall(`{"pattern":"*"}`, meta)

	toolBlock := latestToolBlock(m.blocks)
	if toolBlock == nil {
		t.Fatalf("expected tool block")
	}
	if toolBlock.ToolState != toolExecutionDone {
		t.Fatalf("expected tool to remain done after extra args chunk, got %d", toolBlock.ToolState)
	}
	if m.hasPendingToolResults() {
		t.Fatalf("expected no pending tool results after completion")
	}
}

func TestSyncToolSpinnerStopsWhenEmptyResultArrives(t *testing.T) {
	m := newTestModel()
	meta := map[string]string{"tool": "glob", "call_id": "call-spinner"}

	m.appendToolCall(`{"path":"/","pattern":"**/*"}`, meta)
	if !m.hasPendingToolResults() {
		t.Fatalf("expected pending tool before result")
	}
	if cmd := m.syncToolSpinner(); cmd == nil {
		t.Fatalf("expected spinner tick command to be scheduled while pending")
	}
	if !m.toolSpinnerActive {
		t.Fatalf("expected spinner to be active while pending")
	}

	m.appendToolResult("", meta)
	if m.hasPendingToolResults() {
		t.Fatalf("expected pending tool to clear after empty result")
	}
	if cmd := m.syncToolSpinner(); cmd != nil {
		t.Fatalf("expected no spinner command after completion")
	}
	if m.toolSpinnerActive {
		t.Fatalf("expected spinner to be inactive after completion")
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
