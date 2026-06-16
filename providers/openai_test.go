package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
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

func responseOutputItem(t *testing.T, raw string) responses.ResponseOutputItemUnion {
	t.Helper()
	var item responses.ResponseOutputItemUnion
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("unmarshal output item: %v", err)
	}
	return item
}

func receiveMsg(t *testing.T, ch <-chan Msg) Msg {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for provider message")
		return Msg{}
	}
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

	if msg1.Type != MsgTypeToolCall || msg1.ToolCall == nil || msg1.ToolCall.CallID != "call-1" {
		t.Fatalf("unexpected msg1: %#v", msg1)
	}
	if msg2.Type != MsgTypeToolCall || msg2.ToolCall == nil || msg2.ToolCall.CallID != "call-2" {
		t.Fatalf("unexpected msg2: %#v", msg2)
	}

	close(releaseB)

	select {
	case msg := <-out:
		if msg.Type != MsgTypeToolResult || msg.ToolCall == nil || msg.ToolCall.CallID != "call-2" {
			t.Fatalf("unexpected tool result after releasing tool_b: %#v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for tool_b result")
	}

	close(releaseA)

	select {
	case msg := <-out:
		if msg.Type != MsgTypeToolResult || msg.ToolCall == nil || msg.ToolCall.CallID != "call-1" {
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

func TestToolBatchingInstructionMentionsSandboxDiscovery(t *testing.T) {
	if !strings.Contains(toolBatchingInstruction, "Do not assume /mnt/data exists") {
		t.Fatalf("expected /mnt/data guidance, got: %q", toolBatchingInstruction)
	}
	if !strings.Contains(toolBatchingInstruction, `glob(path:"/", pattern:"*")`) {
		t.Fatalf("expected sandbox discovery example, got: %q", toolBatchingInstruction)
	}
}

func TestOpenAIState(t *testing.T) {
	state := newOpenAIState()
	if state.get() != "" {
		t.Fatal("expected empty state")
	}
	state.set("abc")
	if state.get() != "abc" {
		t.Fatalf("expected abc, got %q", state.get())
	}
}

func TestEmitFinalStreamMessagesUsesCompletedResponse(t *testing.T) {
	resp := &responses.Response{Output: []responses.ResponseOutputItemUnion{
		responseOutputItem(t, `{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello from final"}]}`),
		responseOutputItem(t, `{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"reasoning final"}]}`),
	}}

	out := make(chan Msg, 4)
	emitFinalStreamMessages(out, resp, "hello delta", "reasoning delta", nil)

	first := receiveMsg(t, out)
	second := receiveMsg(t, out)
	if first.Type != MsgTypeChatFinal || first.Value != "Hello from final" {
		t.Fatalf("unexpected first final message: %#v", first)
	}
	if second.Type != MsgTypeReasoningSummaryFinal || second.Value != "reasoning final" {
		t.Fatalf("unexpected second final message: %#v", second)
	}
}

func TestEmitFinalStreamMessagesFallsBackToDeltas(t *testing.T) {
	out := make(chan Msg, 4)
	emitFinalStreamMessages(out, nil, "hello delta", "reasoning delta", nil)

	first := receiveMsg(t, out)
	second := receiveMsg(t, out)
	if first.Type != MsgTypeChatFinal || first.Value != "hello delta" {
		t.Fatalf("unexpected first fallback final message: %#v", first)
	}
	if second.Type != MsgTypeReasoningSummaryFinal || second.Value != "reasoning delta" {
		t.Fatalf("unexpected second fallback final message: %#v", second)
	}
}

func TestOpenAIContextUsageMsgUsesInputTokens(t *testing.T) {
	provider := &OpenAI{maxContextTokens: 1000}
	response := responseWithUsage(t, "resp-usage", 123, 45, 168)

	msg := provider.contextUsageMsg(response)
	if msg == nil {
		t.Fatal("expected context usage message")
	}
	if msg.Type != MsgTypeContextUsage {
		t.Fatalf("unexpected message type: %v", msg.Type)
	}
	if msg.Value != "12.3%" {
		t.Fatalf("unexpected context usage value: %q", msg.Value)
	}
	if msg.Usage == nil {
		t.Fatal("expected typed usage payload")
	}
	if msg.Usage.InputTokensUsed != 123 {
		t.Fatalf("unexpected input tokens used: %d", msg.Usage.InputTokensUsed)
	}
	if msg.Usage.OutputTokensUsed != 45 {
		t.Fatalf("unexpected output tokens used: %d", msg.Usage.OutputTokensUsed)
	}
	if msg.Usage.TotalTokensUsed != 168 {
		t.Fatalf("unexpected total tokens used: %d", msg.Usage.TotalTokensUsed)
	}
	if msg.Usage.ContextWindow != 1000 {
		t.Fatalf("unexpected context window: %d", msg.Usage.ContextWindow)
	}
}

type scriptedResponseStream struct {
	events []responses.ResponseStreamEventUnion
	idx    int
	err    error
}

func (s *scriptedResponseStream) Next() bool {
	if s.idx >= len(s.events) {
		return false
	}
	s.idx++
	return true
}

func (s *scriptedResponseStream) Current() responses.ResponseStreamEventUnion {
	if s.idx == 0 || s.idx > len(s.events) {
		return responses.ResponseStreamEventUnion{}
	}
	return s.events[s.idx-1]
}

func (s *scriptedResponseStream) Err() error {
	return s.err
}

type scriptedOpenAIClient struct {
	mu      sync.Mutex
	streams []responseStream
	params  []responses.ResponseNewParams
}

func (s *scriptedOpenAIClient) ListModels(ctx context.Context) ([]string, error) {
	_ = ctx
	return nil, nil
}

func (s *scriptedOpenAIClient) NewStreamingResponse(ctx context.Context, params responses.ResponseNewParams) responseStream {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.params = append(s.params, params)
	if len(s.streams) == 0 {
		return &scriptedResponseStream{}
	}
	stream := s.streams[0]
	s.streams = s.streams[1:]
	return stream
}

func outputDeltaEvent(text string) responses.ResponseStreamEventUnion {
	return responses.ResponseStreamEventUnion{Type: "response.output_text.delta", Delta: text}
}

func completedEvent(id string) responses.ResponseStreamEventUnion {
	return responses.ResponseStreamEventUnion{Type: "response.completed", Response: responses.Response{ID: id}}
}

func completedEventWithUsage(t *testing.T, id string, inputTokens, outputTokens, totalTokens int64) responses.ResponseStreamEventUnion {
	t.Helper()
	raw := fmt.Sprintf(`{"type":"response.completed","response":{"id":%q,"object":"response","output":[],"usage":{"input_tokens":%d,"input_tokens_details":{"cached_tokens":0},"output_tokens":%d,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":%d}}}`,
		id,
		inputTokens,
		outputTokens,
		totalTokens,
	)
	var event responses.ResponseStreamEventUnion
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatalf("unmarshal completed event with usage: %v", err)
	}
	return event
}

func responseWithUsage(t *testing.T, id string, inputTokens, outputTokens, totalTokens int64) *responses.Response {
	t.Helper()
	raw := fmt.Sprintf(`{"id":%q,"object":"response","output":[],"usage":{"input_tokens":%d,"input_tokens_details":{"cached_tokens":0},"output_tokens":%d,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":%d}}`,
		id,
		inputTokens,
		outputTokens,
		totalTokens,
	)
	var response responses.Response
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		t.Fatalf("unmarshal response with usage: %v", err)
	}
	return &response
}

type scriptedCompressor struct {
	prompt string
}

func (c scriptedCompressor) Compress(ctx context.Context, provider Provider, sessionID string) (string, error) {
	if provider == nil {
		return "", errors.New("provider is required")
	}
	streamCtx := WithSessionID(ctx, sessionID)
	stream := provider.Chat(streamCtx, c.prompt)
	var (
		delta strings.Builder
		final strings.Builder
	)
	for msg := range stream {
		switch msg.Type {
		case MsgTypeChatDelta:
			delta.WriteString(msg.Value)
		case MsgTypeChatFinal:
			final.WriteString(msg.Value)
		case MsgTypeError:
			errText := strings.TrimSpace(msg.Value)
			if errText == "" {
				errText = "provider returned an empty error"
			}
			return "", errors.New(errText)
		}
	}
	output := strings.TrimSpace(final.String())
	if output == "" {
		output = strings.TrimSpace(delta.String())
	}
	if output == "" {
		return "", errors.New("empty compression output")
	}
	return output, nil
}

type failingAfterStreamCompressor struct {
	prompt string
}

func (c failingAfterStreamCompressor) Compress(ctx context.Context, provider Provider, sessionID string) (string, error) {
	_, _ = scriptedCompressor{prompt: c.prompt}.Compress(ctx, provider, sessionID)
	return "", errors.New("compress failed")
}

func TestOpenAIPerformCompressionResetsAndSeedsSession(t *testing.T) {
	client := &scriptedOpenAIClient{streams: []responseStream{
		&scriptedResponseStream{events: []responses.ResponseStreamEventUnion{
			outputDeltaEvent("compressed summary"),
			completedEventWithUsage(t, "resp-compress", 28000, 900, 28900),
		}},
		&scriptedResponseStream{events: []responses.ResponseStreamEventUnion{
			outputDeltaEvent("OK"),
			completedEventWithUsage(t, "resp-seed", 24000, 120, 24120),
		}},
	}}
	provider := &OpenAI{
		client:           client,
		state:            newOpenAIState(),
		sessionID:        "session-1",
		model:            "gpt-5.2",
		maxContextTokens: 400000,
		toolRunner:       parallelToolRunner{},
	}
	provider.state.set("prev-1")
	statusMsg := Msg{}
	ctx := WithCompressionStatusTarget(context.Background(), &statusMsg)

	summary, err := provider.PerformCompression(ctx, "session-1", scriptedCompressor{prompt: "compress to 80 words"})
	if err != nil {
		t.Fatalf("unexpected compression error: %v", err)
	}
	if summary != "compressed summary" {
		t.Fatalf("unexpected compressed summary: %q", summary)
	}
	if got := provider.state.get(); got != "resp-seed" {
		t.Fatalf("expected seeded response id, got %q", got)
	}
	if statusMsg.Type != MsgTypeCompressionStatus {
		t.Fatalf("expected compression status message, got %#v", statusMsg)
	}
	if statusMsg.Value != "Context compressed from 28000 (93.0% left) to 24000 (94.0% left)." {
		t.Fatalf("unexpected compression status text: %q", statusMsg.Value)
	}
	if statusMsg.Compaction == nil {
		t.Fatal("expected typed compaction payload")
	}
	if statusMsg.Compaction.FromTokensUsed != 28000 {
		t.Fatalf("unexpected from tokens used: %d", statusMsg.Compaction.FromTokensUsed)
	}
	if statusMsg.Compaction.ToTokensUsed != 24000 {
		t.Fatalf("unexpected to tokens used: %d", statusMsg.Compaction.ToTokensUsed)
	}
	if statusMsg.Compaction.FromPercentLeft != 93.0 {
		t.Fatalf("unexpected from percent left: %v", statusMsg.Compaction.FromPercentLeft)
	}
	if statusMsg.Compaction.ToPercentLeft != 94.0 {
		t.Fatalf("unexpected to percent left: %v", statusMsg.Compaction.ToPercentLeft)
	}

	if len(client.params) != 2 {
		t.Fatalf("expected 2 response requests, got %d", len(client.params))
	}

	firstJSON, err := json.Marshal(client.params[0])
	if err != nil {
		t.Fatalf("marshal first params: %v", err)
	}
	firstText := string(firstJSON)
	if !strings.Contains(firstText, "\"previous_response_id\":\"prev-1\"") {
		t.Fatalf("expected first request to use previous response id, got %s", firstText)
	}
	if !strings.Contains(firstText, "compress to 80 words") {
		t.Fatalf("expected compression prompt with requested limit, got %s", firstText)
	}

	secondJSON, err := json.Marshal(client.params[1])
	if err != nil {
		t.Fatalf("marshal second params: %v", err)
	}
	secondText := string(secondJSON)
	if strings.Contains(secondText, "\"previous_response_id\"") {
		t.Fatalf("expected seeded request to start a fresh context, got %s", secondText)
	}
	if !strings.Contains(secondText, "compressed summary") {
		t.Fatalf("expected seeded request to include compressed summary, got %s", secondText)
	}
}

func TestOpenAIPerformCompressionRestoresStateOnInjectionFailure(t *testing.T) {
	client := &scriptedOpenAIClient{streams: []responseStream{
		&scriptedResponseStream{events: []responses.ResponseStreamEventUnion{
			outputDeltaEvent("compressed summary"),
			completedEvent("resp-compress"),
		}},
		&scriptedResponseStream{err: errors.New("inject failed")},
	}}
	provider := &OpenAI{
		client:     client,
		state:      newOpenAIState(),
		sessionID:  "session-1",
		model:      "gpt-5.2",
		toolRunner: parallelToolRunner{},
	}
	provider.state.set("prev-1")

	_, err := provider.PerformCompression(context.Background(), "session-1", scriptedCompressor{prompt: "compress to 60 words"})
	if err == nil {
		t.Fatal("expected compression injection error")
	}
	if !strings.Contains(err.Error(), "compression injection failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := provider.state.get(); got != "prev-1" {
		t.Fatalf("expected previous state to be restored, got %q", got)
	}
}

func TestOpenAIPerformCompressionRestoresStateOnCompressorFailure(t *testing.T) {
	client := &scriptedOpenAIClient{streams: []responseStream{
		&scriptedResponseStream{events: []responses.ResponseStreamEventUnion{
			outputDeltaEvent("compressed summary"),
			completedEvent("resp-compress"),
		}},
	}}
	provider := &OpenAI{
		client:     client,
		state:      newOpenAIState(),
		sessionID:  "session-1",
		model:      "gpt-5.2",
		toolRunner: parallelToolRunner{},
	}
	provider.state.set("prev-1")

	_, err := provider.PerformCompression(context.Background(), "session-1", failingAfterStreamCompressor{prompt: "compress prompt"})
	if err == nil {
		t.Fatal("expected compressor error")
	}
	if got := provider.state.get(); got != "prev-1" {
		t.Fatalf("expected previous state to be restored after compressor failure, got %q", got)
	}
	if len(client.params) != 1 {
		t.Fatalf("expected only compressor request to run, got %d", len(client.params))
	}
}
