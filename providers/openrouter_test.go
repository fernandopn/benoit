package providers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fernandopn/benoit/tools"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

type scriptedChatStream struct {
	chunks []openai.ChatCompletionChunk
	idx    int
	err    error
}

func (s *scriptedChatStream) Next() bool {
	if s.idx >= len(s.chunks) {
		return false
	}
	s.idx++
	return true
}

func (s *scriptedChatStream) Current() openai.ChatCompletionChunk {
	if s.idx == 0 || s.idx > len(s.chunks) {
		return openai.ChatCompletionChunk{}
	}
	return s.chunks[s.idx-1]
}

func (s *scriptedChatStream) Err() error {
	return s.err
}

type scriptedChatClient struct {
	mu               sync.Mutex
	streams          []chatCompletionStream
	params           []openai.ChatCompletionNewParams
	models           []string
	contextLength    int64
	contextLengthErr error
}

func (c *scriptedChatClient) ListModels(ctx context.Context) ([]string, error) {
	_ = ctx
	return c.models, nil
}

func (c *scriptedChatClient) ModelContextLength(ctx context.Context, model string) (int64, error) {
	_ = ctx
	_ = model
	return c.contextLength, c.contextLengthErr
}

func (c *scriptedChatClient) NewStreamingChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) chatCompletionStream {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	c.params = append(c.params, params)
	if len(c.streams) == 0 {
		return &scriptedChatStream{}
	}
	stream := c.streams[0]
	c.streams = c.streams[1:]
	return stream
}

func chatChunk(t *testing.T, raw string) openai.ChatCompletionChunk {
	t.Helper()
	var chunk openai.ChatCompletionChunk
	if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
		t.Fatalf("unmarshal chat chunk: %v", err)
	}
	return chunk
}

type stubFunctionTool struct {
	name   string
	output string
}

func (s stubFunctionTool) Name() string {
	return s.name
}

func (s stubFunctionTool) Definition() responses.ToolUnionParam {
	return responses.ToolUnionParam{OfFunction: &responses.FunctionToolParam{Name: s.name}}
}

func (s stubFunctionTool) Call(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	_ = args
	return s.output, nil
}

func collectMsgs(t *testing.T, ch <-chan Msg) []Msg {
	t.Helper()
	var msgs []Msg
	timeout := time.After(5 * time.Second)
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return msgs
			}
			msgs = append(msgs, msg)
		case <-timeout:
			t.Fatal("timed out collecting messages")
			return msgs
		}
	}
}

func newTestOpenRouter(client chatCompletionsClient) *OpenRouter {
	return &OpenRouter{
		client:     client,
		state:      newOpenRouterState(),
		sessionID:  "session-1",
		model:      "test-model",
		toolRunner: parallelToolRunner{},
	}
}

func TestOpenRouterChatStreamsAndPersistsHistory(t *testing.T) {
	client := &scriptedChatClient{streams: []chatCompletionStream{
		&scriptedChatStream{chunks: []openai.ChatCompletionChunk{
			chatChunk(t, `{"id":"c1","object":"chat.completion.chunk","created":0,"model":"m","choices":[{"index":0,"finish_reason":"","delta":{"content":"Hello","reasoning":"thinking"}}]}`),
			chatChunk(t, `{"id":"c1","object":"chat.completion.chunk","created":0,"model":"m","choices":[{"index":0,"finish_reason":"stop","delta":{"content":" world"}}]}`),
			chatChunk(t, `{"id":"c1","object":"chat.completion.chunk","created":0,"model":"m","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}}`),
		}},
	}}
	provider := newTestOpenRouter(client)

	msgs := collectMsgs(t, provider.Chat(context.Background(), "hi"))

	var chatDeltas, reasoningDeltas []string
	var chatFinal string
	sawContextUsage := false
	for _, msg := range msgs {
		switch msg.Type {
		case MsgTypeChatDelta:
			chatDeltas = append(chatDeltas, msg.Value)
		case MsgTypeReasoningSummaryDelta:
			reasoningDeltas = append(reasoningDeltas, msg.Value)
		case MsgTypeChatFinal:
			chatFinal = msg.Value
		case MsgTypeContextUsage:
			sawContextUsage = true
		case MsgTypeError:
			t.Fatalf("unexpected error message: %q", msg.Value)
		}
	}

	if strings.Join(chatDeltas, "") != "Hello world" {
		t.Fatalf("unexpected chat deltas: %v", chatDeltas)
	}
	if strings.Join(reasoningDeltas, "") != "thinking" {
		t.Fatalf("unexpected reasoning deltas: %v", reasoningDeltas)
	}
	if chatFinal != "Hello world" {
		t.Fatalf("unexpected chat final: %q", chatFinal)
	}
	if !sawContextUsage {
		t.Fatal("expected a context usage message")
	}

	serialized, err := provider.ExportPreviousResponse()
	if err != nil {
		t.Fatalf("export previous response: %v", err)
	}
	var previous OpenRouterPreviousResponse
	if err := json.Unmarshal([]byte(serialized), &previous); err != nil {
		t.Fatalf("unmarshal previous response: %v", err)
	}
	if len(previous.Messages) != 2 {
		t.Fatalf("expected user+assistant history, got %d messages", len(previous.Messages))
	}
	if previous.Messages[0].Role != roleUser || previous.Messages[0].Content != "hi" {
		t.Fatalf("unexpected user message: %#v", previous.Messages[0])
	}
	if previous.Messages[1].Role != roleAssistant || previous.Messages[1].Content != "Hello world" {
		t.Fatalf("unexpected assistant message: %#v", previous.Messages[1])
	}
}

func TestOpenRouterChatRunsToolCalls(t *testing.T) {
	client := &scriptedChatClient{streams: []chatCompletionStream{
		&scriptedChatStream{chunks: []openai.ChatCompletionChunk{
			chatChunk(t, `{"id":"c1","object":"chat.completion.chunk","created":0,"model":"m","choices":[{"index":0,"finish_reason":"tool_calls","delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"tool_a","arguments":"{\"x\":1}"}}]}}]}`),
			chatChunk(t, `{"id":"c1","object":"chat.completion.chunk","created":0,"model":"m","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`),
		}},
		&scriptedChatStream{chunks: []openai.ChatCompletionChunk{
			chatChunk(t, `{"id":"c2","object":"chat.completion.chunk","created":0,"model":"m","choices":[{"index":0,"finish_reason":"stop","delta":{"content":"done"}}]}`),
			chatChunk(t, `{"id":"c2","object":"chat.completion.chunk","created":0,"model":"m","choices":[],"usage":{"prompt_tokens":20,"completion_tokens":1,"total_tokens":21}}`),
		}},
	}}
	provider := newTestOpenRouter(client)
	provider.toolMap = map[string]tools.Tool{"tool_a": stubFunctionTool{name: "tool_a", output: "result-a"}}

	msgs := collectMsgs(t, provider.Chat(context.Background(), "use the tool"))

	var (
		sawToolCall   bool
		sawToolResult bool
		chatFinal     string
	)
	for _, msg := range msgs {
		switch msg.Type {
		case MsgTypeToolCall:
			if msg.ToolCall != nil && msg.ToolCall.CallID == "call-1" && msg.ToolCall.Name == "tool_a" {
				sawToolCall = true
			}
		case MsgTypeToolResult:
			if msg.Value == "result-a" && msg.ToolCall != nil && msg.ToolCall.CallID == "call-1" {
				sawToolResult = true
			}
		case MsgTypeChatFinal:
			chatFinal = msg.Value
		case MsgTypeError:
			t.Fatalf("unexpected error message: %q", msg.Value)
		}
	}

	if !sawToolCall {
		t.Fatal("expected a tool call message")
	}
	if !sawToolResult {
		t.Fatal("expected a tool result message")
	}
	if chatFinal != "done" {
		t.Fatalf("unexpected chat final: %q", chatFinal)
	}

	if len(client.params) != 2 {
		t.Fatalf("expected 2 chat completion requests, got %d", len(client.params))
	}
	secondJSON, err := json.Marshal(client.params[1])
	if err != nil {
		t.Fatalf("marshal second params: %v", err)
	}
	secondText := string(secondJSON)
	if !strings.Contains(secondText, "result-a") {
		t.Fatalf("expected second request to include tool result, got %s", secondText)
	}
	if !strings.Contains(secondText, "call-1") {
		t.Fatalf("expected second request to include the tool call id, got %s", secondText)
	}
}

func TestOpenRouterContextUsageWithKnownWindow(t *testing.T) {
	client := &scriptedChatClient{streams: []chatCompletionStream{
		&scriptedChatStream{chunks: []openai.ChatCompletionChunk{
			chatChunk(t, `{"id":"c1","object":"chat.completion.chunk","created":0,"model":"m","choices":[{"index":0,"finish_reason":"stop","delta":{"content":"ok"}}]}`),
			chatChunk(t, `{"id":"c1","object":"chat.completion.chunk","created":0,"model":"m","choices":[],"usage":{"prompt_tokens":123,"completion_tokens":45,"total_tokens":168}}`),
		}},
	}}
	provider := newTestOpenRouter(client)
	provider.maxContextTokens = 1000

	msgs := collectMsgs(t, provider.Chat(context.Background(), "hi"))

	var usage *Msg
	for i := range msgs {
		if msgs[i].Type == MsgTypeContextUsage {
			usage = &msgs[i]
		}
	}
	if usage == nil {
		t.Fatal("expected a context usage message")
	}
	if usage.Value != "12.3%" {
		t.Fatalf("unexpected context usage value: %q", usage.Value)
	}
	if usage.Usage == nil {
		t.Fatal("expected typed usage payload")
	}
	if usage.Usage.InputTokensUsed != 123 {
		t.Fatalf("unexpected input tokens used: %d", usage.Usage.InputTokensUsed)
	}
	if usage.Usage.ContextWindow != 1000 {
		t.Fatalf("unexpected context window: %d", usage.Usage.ContextWindow)
	}
	if usage.Usage.OutputTokensUsed != 45 {
		t.Fatalf("unexpected output tokens used: %d", usage.Usage.OutputTokensUsed)
	}
}

func TestOpenRouterExportImportRoundTrip(t *testing.T) {
	source := newTestOpenRouter(&scriptedChatClient{})
	source.state.append(
		chatMessage{Role: roleUser, Content: "hi"},
		chatMessage{Role: roleAssistant, Content: "", ToolCalls: []chatToolCall{{ID: "call-1", Name: "tool_a", Arguments: "{}"}}},
		chatMessage{Role: roleTool, Content: "result-a", ToolCallID: "call-1"},
		chatMessage{Role: roleAssistant, Content: "done"},
	)

	serialized, err := source.ExportPreviousResponse()
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	restored := newTestOpenRouter(&scriptedChatClient{})
	if err := restored.ImportPreviousResponse(serialized); err != nil {
		t.Fatalf("import: %v", err)
	}

	want := source.state.snapshot()
	got := restored.state.snapshot()
	if len(want) != len(got) {
		t.Fatalf("expected %d messages, got %d", len(want), len(got))
	}
	for i := range want {
		if want[i].Role != got[i].Role || want[i].Content != got[i].Content || want[i].ToolCallID != got[i].ToolCallID {
			t.Fatalf("message %d mismatch: want %#v got %#v", i, want[i], got[i])
		}
		if len(want[i].ToolCalls) != len(got[i].ToolCalls) {
			t.Fatalf("message %d tool call count mismatch", i)
		}
		for j := range want[i].ToolCalls {
			if want[i].ToolCalls[j] != got[i].ToolCalls[j] {
				t.Fatalf("message %d tool call %d mismatch: want %#v got %#v", i, j, want[i].ToolCalls[j], got[i].ToolCalls[j])
			}
		}
	}
}

func TestOpenRouterEmptyExportAndImport(t *testing.T) {
	provider := newTestOpenRouter(&scriptedChatClient{})
	serialized, err := provider.ExportPreviousResponse()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if serialized != "" {
		t.Fatalf("expected empty export, got %q", serialized)
	}
	if err := provider.ImportPreviousResponse(""); err != nil {
		t.Fatalf("import empty: %v", err)
	}
	if len(provider.state.snapshot()) != 0 {
		t.Fatal("expected empty history after importing empty cursor")
	}
}

func TestContextLengthFromModelJSON(t *testing.T) {
	if got := contextLengthFromModelJSON(`{"id":"z-ai/glm-5.1","context_length":202752}`); got != 202752 {
		t.Fatalf("expected root context_length, got %d", got)
	}
	if got := contextLengthFromModelJSON(`{"id":"m","top_provider":{"context_length":131072}}`); got != 131072 {
		t.Fatalf("expected top_provider fallback, got %d", got)
	}
	if got := contextLengthFromModelJSON(`{"id":"m"}`); got != 0 {
		t.Fatalf("expected 0 when absent, got %d", got)
	}
	if got := contextLengthFromModelJSON(""); got != 0 {
		t.Fatalf("expected 0 for empty raw, got %d", got)
	}
}

func TestOpenRouterResolveContextWindow(t *testing.T) {
	provider := newTestOpenRouter(&scriptedChatClient{contextLength: 200000})
	provider.resolveContextWindow(context.Background())
	if provider.maxContextTokens != 200000 {
		t.Fatalf("expected resolved context window 200000, got %d", provider.maxContextTokens)
	}

	failing := newTestOpenRouter(&scriptedChatClient{contextLengthErr: errors.New("boom")})
	failing.resolveContextWindow(context.Background())
	if failing.maxContextTokens != 0 {
		t.Fatalf("expected context window to stay unset on error, got %d", failing.maxContextTokens)
	}
}

func TestOpenRouterContextUsageUsesResolvedWindow(t *testing.T) {
	client := &scriptedChatClient{
		contextLength: 1000,
		streams: []chatCompletionStream{
			&scriptedChatStream{chunks: []openai.ChatCompletionChunk{
				chatChunk(t, `{"id":"c1","object":"chat.completion.chunk","created":0,"model":"m","choices":[{"index":0,"finish_reason":"stop","delta":{"content":"ok"}}]}`),
				chatChunk(t, `{"id":"c1","object":"chat.completion.chunk","created":0,"model":"m","choices":[],"usage":{"prompt_tokens":123,"completion_tokens":45,"total_tokens":168}}`),
			}},
		},
	}
	provider := newTestOpenRouter(client)
	provider.resolveContextWindow(context.Background())

	msgs := collectMsgs(t, provider.Chat(context.Background(), "hi"))
	var usage *Msg
	for i := range msgs {
		if msgs[i].Type == MsgTypeContextUsage {
			usage = &msgs[i]
		}
	}
	if usage == nil {
		t.Fatal("expected a context usage message")
	}
	if usage.Value != "12.3%" {
		t.Fatalf("unexpected context usage value: %q", usage.Value)
	}
	if usage.Usage == nil || usage.Usage.ContextWindow != 1000 {
		t.Fatalf("unexpected context window: %#v", usage.Usage)
	}
}

func TestOpenRouterInitToolsSkipsHostedTools(t *testing.T) {
	provider := newTestOpenRouter(&scriptedChatClient{})
	toolSet := []tools.Tool{
		tools.NewOpenAICodeInterpreterTool(),
		tools.NewOpenAIWebSearchTool(),
		stubFunctionTool{name: "tool_a"},
	}
	if err := provider.initTools(toolSet); err != nil {
		t.Fatalf("init tools: %v", err)
	}
	if len(provider.toolDefs) != 1 {
		t.Fatalf("expected only the function tool definition, got %d", len(provider.toolDefs))
	}
	if _, ok := provider.toolMap["tool_a"]; !ok {
		t.Fatal("expected tool_a to be registered for local dispatch")
	}
	if _, ok := provider.toolMap["code_interpreter"]; ok {
		t.Fatal("did not expect hosted code_interpreter tool to be registered")
	}
}
