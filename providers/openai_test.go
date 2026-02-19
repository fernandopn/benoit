package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/fernandopn/benoit/tools"
	"github.com/openai/openai-go/v3/responses"
)

type blockingTool struct {
	name    string
	startCh chan<- string
	release <-chan struct{}
	output  string
}

func (b *blockingTool) Name() string {
	return b.name
}

func (b *blockingTool) Definition() responses.ToolUnionParam {
	return responses.ToolUnionParam{}
}

func (b *blockingTool) Call(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	_ = args
	b.startCh <- b.name
	<-b.release
	return b.output, nil
}

func functionCallItem(t *testing.T, name, callID string, args map[string]any) responses.ResponseOutputItemUnion {
	t.Helper()
	payload, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	raw := fmt.Sprintf(`{"type":"function_call","name":%q,"call_id":%q,"arguments":%q}`, name, callID, string(payload))
	var item responses.ResponseOutputItemUnion
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("unmarshal item: %v", err)
	}
	return item
}

func functionCallItemRawArgs(t *testing.T, name, callID, rawArgs string) responses.ResponseOutputItemUnion {
	t.Helper()
	raw := fmt.Sprintf(`{"type":"function_call","name":%q,"call_id":%q,"arguments":%q}`, name, callID, rawArgs)
	var item responses.ResponseOutputItemUnion
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("unmarshal item: %v", err)
	}
	return item
}

func TestToolOutputsFromResponseParallelAndOrdered(t *testing.T) {
	startCh := make(chan string, 2)
	releaseA := make(chan struct{})
	releaseB := make(chan struct{})

	toolA := &blockingTool{name: "tool_a", startCh: startCh, release: releaseA, output: "out-a"}
	toolB := &blockingTool{name: "tool_b", startCh: startCh, release: releaseB, output: "out-b"}
	base := &OpenAI{
		toolMap:    map[string]tools.Tool{"tool_a": toolA, "tool_b": toolB},
		toolRunner: parallelToolRunner{},
	}

	resp := &responses.Response{
		Output: []responses.ResponseOutputItemUnion{
			functionCallItem(t, "tool_a", "call-1", map[string]any{"x": 1}),
			functionCallItem(t, "tool_b", "call-2", map[string]any{"y": 2}),
		},
	}

	out := make(chan Msg, 10)
	var (
		toolOutputs responses.ResponseInputParam
		err         error
	)
	done := make(chan struct{})
	go func() {
		toolOutputs, err = base.toolOutputsFromResponse(context.Background(), resp, out)
		close(done)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-startCh:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for tools to start")
		}
	}

	msg1 := <-out
	msg2 := <-out

	if msg1.Type != MsgTypeToolCall || msg1.Metadata["call_id"] != "call-1" {
		t.Fatalf("unexpected msg1: %#v", msg1)
	}
	if msg2.Type != MsgTypeToolCall || msg2.Metadata["call_id"] != "call-2" {
		t.Fatalf("unexpected msg2: %#v", msg2)
	}

	close(releaseB)

	select {
	case msg := <-out:
		if msg.Type != MsgTypeToolResult || msg.Metadata["call_id"] != "call-2" {
			t.Fatalf("unexpected tool result after releasing tool_b: %#v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for tool_b result")
	}

	close(releaseA)

	select {
	case msg := <-out:
		if msg.Type != MsgTypeToolResult || msg.Metadata["call_id"] != "call-1" {
			t.Fatalf("unexpected tool result after releasing tool_a: %#v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for tool_a result")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for tool outputs")
	}

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toolOutputs) != 2 {
		t.Fatalf("expected 2 tool outputs, got %d", len(toolOutputs))
	}
	if toolOutputs[0].OfFunctionCallOutput == nil || toolOutputs[1].OfFunctionCallOutput == nil {
		t.Fatalf("expected function call output params")
	}
	if toolOutputs[0].OfFunctionCallOutput.CallID != "call-1" {
		t.Fatalf("expected call-1 first, got %q", toolOutputs[0].OfFunctionCallOutput.CallID)
	}
	if toolOutputs[1].OfFunctionCallOutput.CallID != "call-2" {
		t.Fatalf("expected call-2 second, got %q", toolOutputs[1].OfFunctionCallOutput.CallID)
	}
	if toolOutputs[0].OfFunctionCallOutput.Output.OfString.Value != "out-a" {
		t.Fatalf("unexpected output for call-1: %q", toolOutputs[0].OfFunctionCallOutput.Output.OfString.Value)
	}
	if toolOutputs[1].OfFunctionCallOutput.Output.OfString.Value != "out-b" {
		t.Fatalf("unexpected output for call-2: %q", toolOutputs[1].OfFunctionCallOutput.Output.OfString.Value)
	}
}

func TestToolOutputsFromResponseErrors(t *testing.T) {
	base := &OpenAI{toolMap: map[string]tools.Tool{}}
	resp := &responses.Response{Output: []responses.ResponseOutputItemUnion{
		functionCallItem(t, "missing_tool", "call-1", map[string]any{}),
	}}
	_, err := base.toolOutputsFromResponse(context.Background(), resp, make(chan Msg, 1))
	if err == nil {
		t.Fatal("expected error for missing tool")
	}

	base.toolMap = map[string]tools.Tool{"tool_a": &blockingTool{name: "tool_a"}}
	resp = &responses.Response{Output: []responses.ResponseOutputItemUnion{
		functionCallItemRawArgs(t, "tool_a", "call-1", "not-json"),
	}}
	_, err = base.toolOutputsFromResponse(context.Background(), resp, make(chan Msg, 1))
	if err == nil {
		t.Fatal("expected error for invalid arguments")
	}
}

func TestFunctionCallsFromResponse(t *testing.T) {
	resp := &responses.Response{Output: []responses.ResponseOutputItemUnion{
		functionCallItem(t, "tool_a", "call-1", map[string]any{"x": 1}),
		functionCallItem(t, "", "call-2", map[string]any{"y": 2}),
	}}
	calls := functionCallsFromResponse(resp)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].name != "tool_a" || calls[0].callID != "call-1" {
		t.Fatalf("unexpected call: %#v", calls[0])
	}
}

func TestBuildToolCallsValidation(t *testing.T) {
	calls := []functionCall{{name: "tool_a", callID: ""}}
	_, err := buildToolCalls(calls, map[string]tools.Tool{"tool_a": &blockingTool{name: "tool_a"}})
	if err == nil {
		t.Fatal("expected error for missing call_id")
	}

	_, err = buildToolCalls(calls, nil)
	if err == nil {
		t.Fatal("expected error for nil tool map")
	}
}

func TestParseToolArgs(t *testing.T) {
	args, err := parseToolArgs("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 0 {
		t.Fatalf("expected empty args, got %v", args)
	}

	args, err = parseToolArgs("{\"a\":1}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args["a"].(float64) != 1 {
		t.Fatalf("unexpected value: %v", args["a"])
	}

	_, err = parseToolArgs("not-json")
	if err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestOpenAIState(t *testing.T) {
	state := newOpenAIState()
	if state.get("") != "" {
		t.Fatal("expected empty state")
	}
	state.set("", "abc")
	if state.get("") != "abc" {
		t.Fatalf("expected abc, got %q", state.get(""))
	}

	state.set("telegram:99", "id-99")
	if state.get("telegram:99") != "id-99" {
		t.Fatalf("expected id-99, got %q", state.get("telegram:99"))
	}
	if state.get("") != "abc" {
		t.Fatalf("default session changed unexpectedly: %q", state.get(""))
	}
}
