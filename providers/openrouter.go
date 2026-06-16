package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/fernandopn/benoit/sessionid"
	"github.com/fernandopn/benoit/tools"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

const openRouterBaseURL = "https://openrouter.ai/api/v1"

const (
	roleSystem    = "system"
	roleUser      = "user"
	roleAssistant = "assistant"
	roleTool      = "tool"
)

// === Abstractions for testing ===

type chatCompletionStream interface {
	Next() bool
	Current() openai.ChatCompletionChunk
	Err() error
}

type chatCompletionsClient interface {
	ListModels(ctx context.Context) ([]string, error)
	// ModelContextLength returns the context window (in tokens) for the model,
	// or 0 when it cannot be determined.
	ModelContextLength(ctx context.Context, model string) (int64, error)
	NewStreamingChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) chatCompletionStream
}

type openRouterClientAdapter struct {
	client openai.Client
}

func newOpenRouterClientAdapter(apiKey string) *openRouterClientAdapter {
	return &openRouterClientAdapter{client: openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(openRouterBaseURL),
	)}
}

func (a *openRouterClientAdapter) ListModels(ctx context.Context) ([]string, error) {
	page, err := a.client.Models.List(ctx)
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(page.Data))
	for _, model := range page.Data {
		if model.ID != "" {
			models = append(models, model.ID)
		}
	}
	return models, nil
}

func (a *openRouterClientAdapter) ModelContextLength(ctx context.Context, model string) (int64, error) {
	page, err := a.client.Models.List(ctx)
	if err != nil {
		return 0, err
	}
	for _, entry := range page.Data {
		if entry.ID == model {
			return contextLengthFromModelJSON(entry.RawJSON()), nil
		}
	}
	return 0, nil
}

func (a *openRouterClientAdapter) NewStreamingChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) chatCompletionStream {
	return a.client.Chat.Completions.NewStreaming(ctx, params)
}

// contextLengthFromModelJSON reads the context window from an OpenRouter model
// entry. OpenRouter exposes context_length at the root and under top_provider.
func contextLengthFromModelJSON(raw string) int64 {
	if raw == "" {
		return 0
	}
	var payload struct {
		ContextLength int64 `json:"context_length"`
		TopProvider   struct {
			ContextLength int64 `json:"context_length"`
		} `json:"top_provider"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return 0
	}
	if payload.ContextLength > 0 {
		return payload.ContextLength
	}
	return payload.TopProvider.ContextLength
}

// chatToolCall is a JSON-serializable tool call kept in local history.
type chatToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// chatMessage is a JSON-serializable conversation message. SDK request param
// unions are not used for storage because they do not round-trip through JSON.
type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// OpenRouterPreviousResponse is the OpenRouter session cursor: the full
// conversation history, since OpenRouter is stateless and requires the history
// to be resent on every request.
type OpenRouterPreviousResponse struct {
	Messages []chatMessage `json:"messages"`
}

func (OpenRouterPreviousResponse) isPreviousResponse() {}

type openRouterState struct {
	mu       sync.Mutex
	messages []chatMessage
}

func newOpenRouterState() *openRouterState {
	return &openRouterState{}
}

func (s *openRouterState) append(messages ...chatMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, messages...)
}

func (s *openRouterState) snapshot() []chatMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]chatMessage, len(s.messages))
	copy(out, s.messages)
	return out
}

func (s *openRouterState) replace(messages []chatMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = messages
}

// OpenRouter uses the OpenAI-compatible Chat Completions streaming API and keeps
// the conversation history locally because the API is stateless.
type OpenRouter struct {
	client           chatCompletionsClient
	state            *openRouterState
	sessionID        string
	model            string
	maxContextTokens int64
	params           OpenAIProviderParams
	toolDefs         []openai.ChatCompletionToolUnionParam
	toolMap          map[string]tools.Tool
	toolRunner       toolRunner
}

var (
	_ Provider              = (*OpenRouter)(nil)
	_ SessionCursorProvider = (*OpenRouter)(nil)
	_ PreviousResponse      = OpenRouterPreviousResponse{}
)

func NewOpenRouter(ctx context.Context, model string, apiKey string, params OpenAIProviderParams, toolSet []tools.Tool) (*OpenRouter, error) {
	return newOpenRouter(ctx, model, apiKey, params, toolSet)
}

func newOpenRouter(ctx context.Context, model string, apiKey string, params OpenAIProviderParams, toolSet []tools.Tool) (*OpenRouter, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("api key is required")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("model is required")
	}

	provider := &OpenRouter{
		client:     newOpenRouterClientAdapter(apiKey),
		state:      newOpenRouterState(),
		sessionID:  sessionid.Normalize(params.SessionID),
		model:      model,
		toolRunner: parallelToolRunner{},
		params:     params,
	}
	if err := provider.ImportPreviousResponse(params.PreviousResponse); err != nil {
		return nil, err
	}
	if err := provider.initTools(toolSet); err != nil {
		return nil, err
	}
	provider.resolveContextWindow(ctx)
	return provider, nil
}

// resolveContextWindow best-effort fetches the model's context window from
// OpenRouter so the UIs can display context usage. Failures leave it unset.
func (o *OpenRouter) resolveContextWindow(ctx context.Context) {
	if ctx == nil {
		return
	}
	length, err := o.client.ModelContextLength(ctx, o.model)
	if err != nil || length <= 0 {
		return
	}
	o.maxContextTokens = length
}

func (o *OpenRouter) initTools(toolSet []tools.Tool) error {
	if len(toolSet) == 0 {
		return nil
	}
	o.toolMap = make(map[string]tools.Tool, len(toolSet))
	o.toolDefs = make([]openai.ChatCompletionToolUnionParam, 0, len(toolSet))
	for _, tool := range toolSet {
		if tool == nil {
			continue
		}
		schema := tool.Schema()
		if err := schema.Validate(); err != nil {
			return err
		}
		def, ok, err := toChatTool(schema)
		if err != nil {
			return err
		}
		if !ok {
			// Hosted tools (for example web_search and code_interpreter) run
			// server-side and are not exposed over the Chat Completions API.
			continue
		}
		if _, exists := o.toolMap[schema.Name]; exists {
			return fmt.Errorf("duplicate tool name: %s", schema.Name)
		}
		o.toolMap[schema.Name] = tool
		o.toolDefs = append(o.toolDefs, def)
	}
	return nil
}

// toChatTool adapts a provider-neutral tool schema into a Chat Completions tool
// param. The bool result reports whether the tool can be exposed over the Chat
// Completions API: only function tools can, so hosted-tool kinds return false.
func toChatTool(schema tools.ToolSchema) (openai.ChatCompletionToolUnionParam, bool, error) {
	if schema.Kind != tools.ToolKindFunction {
		return openai.ChatCompletionToolUnionParam{}, false, nil
	}
	params, err := schema.ParametersMap()
	if err != nil {
		return openai.ChatCompletionToolUnionParam{}, false, err
	}
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        schema.Name,
		Description: openai.String(schema.Description),
		Strict:      openai.Bool(schema.Strict),
		Parameters:  shared.FunctionParameters(params),
	}), true, nil
}

func (o *OpenRouter) ListModels(ctx context.Context) ([]string, error) {
	return o.client.ListModels(ctx)
}

func (o *OpenRouter) Name() string {
	return fmt.Sprintf("OpenRouter %s", o.model)
}

func (o *OpenRouter) ProviderType() ProviderType {
	return ProviderTypeOpenRouter
}

func (o *OpenRouter) SessionID() string {
	return o.sessionID
}

func (o *OpenRouter) ExportPreviousResponse() (string, error) {
	messages := o.state.snapshot()
	if len(messages) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(OpenRouterPreviousResponse{Messages: messages})
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func (o *OpenRouter) ImportPreviousResponse(serialized string) error {
	serialized = strings.TrimSpace(serialized)
	if serialized == "" {
		return nil
	}
	var previous OpenRouterPreviousResponse
	if err := json.Unmarshal([]byte(serialized), &previous); err != nil {
		return err
	}
	o.state.replace(previous.Messages)
	return nil
}

func (o *OpenRouter) Chat(ctx context.Context, input string) <-chan Msg {
	out := make(chan Msg)

	go func() {
		defer close(out)

		o.state.append(chatMessage{Role: roleUser, Content: input})
		for {
			result, err := o.streamCompletion(ctx, o.buildParams(), out)
			if err != nil {
				out <- Msg{Type: MsgTypeError, Value: err.Error()}
				return
			}

			// Append the assistant turn before emitting any final/tool message so
			// the persisted cursor is consistent and complete.
			o.state.append(result.assistantMessage())

			if len(result.toolCalls) == 0 {
				emitFinalChatMessages(out, result.content, result.reasoning, o.finalMessageInfo(result))
				o.emitContextUsage(result, out)
				return
			}

			toolMessages, err := o.runToolCalls(ctx, result.toolCalls, out)
			if err != nil {
				out <- Msg{Type: MsgTypeError, Value: err.Error()}
				return
			}
			o.state.append(toolMessages...)
			o.emitContextUsage(result, out)
		}
	}()

	return out
}

type chatCompletionResult struct {
	content   string
	reasoning string
	toolCalls []chatToolCall
	usage     openai.CompletionUsage
	hasUsage  bool
}

func (r chatCompletionResult) assistantMessage() chatMessage {
	return chatMessage{
		Role:      roleAssistant,
		Content:   r.content,
		ToolCalls: r.toolCalls,
	}
}

func (o *OpenRouter) streamCompletion(ctx context.Context, params openai.ChatCompletionNewParams, out chan<- Msg) (chatCompletionResult, error) {
	stream := o.client.NewStreamingChatCompletion(ctx, params)
	var (
		content   strings.Builder
		reasoning strings.Builder
		toolCalls []*accumulatingToolCall
		usage     openai.CompletionUsage
		hasUsage  bool
	)

	for stream.Next() {
		chunk := stream.Current()
		if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 || chunk.Usage.TotalTokens > 0 {
			usage = chunk.Usage
			hasUsage = true
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			out <- Msg{Type: MsgTypeChatDelta, Value: delta.Content}
			content.WriteString(delta.Content)
		}
		if reasoningText := deltaReasoning(delta); reasoningText != "" {
			out <- Msg{Type: MsgTypeReasoningSummaryDelta, Value: reasoningText}
			reasoning.WriteString(reasoningText)
		}
		for _, toolCall := range delta.ToolCalls {
			accumulateToolCall(&toolCalls, toolCall)
		}
	}
	if err := stream.Err(); err != nil {
		return chatCompletionResult{}, err
	}

	return chatCompletionResult{
		content:   content.String(),
		reasoning: reasoning.String(),
		toolCalls: finalizeToolCalls(toolCalls),
		usage:     usage,
		hasUsage:  hasUsage,
	}, nil
}

// deltaReasoning extracts the non-standard `reasoning` field that OpenRouter adds
// to streaming deltas. It returns an empty string when absent.
func deltaReasoning(delta openai.ChatCompletionChunkChoiceDelta) string {
	field, ok := delta.JSON.ExtraFields["reasoning"]
	if !ok {
		return ""
	}
	raw := strings.TrimSpace(field.Raw())
	if raw == "" || raw == "null" {
		return ""
	}
	var reasoning string
	if err := json.Unmarshal([]byte(raw), &reasoning); err != nil {
		return ""
	}
	return reasoning
}

type accumulatingToolCall struct {
	id        string
	name      string
	arguments strings.Builder
}

func accumulateToolCall(calls *[]*accumulatingToolCall, delta openai.ChatCompletionChunkChoiceDeltaToolCall) {
	index := int(delta.Index)
	for len(*calls) <= index {
		*calls = append(*calls, &accumulatingToolCall{})
	}
	call := (*calls)[index]
	if delta.ID != "" {
		call.id = delta.ID
	}
	if delta.Function.Name != "" {
		call.name = delta.Function.Name
	}
	if delta.Function.Arguments != "" {
		call.arguments.WriteString(delta.Function.Arguments)
	}
}

func finalizeToolCalls(calls []*accumulatingToolCall) []chatToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]chatToolCall, 0, len(calls))
	for _, call := range calls {
		if call == nil || call.name == "" {
			continue
		}
		out = append(out, chatToolCall{
			ID:        call.id,
			Name:      call.name,
			Arguments: call.arguments.String(),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (o *OpenRouter) runToolCalls(ctx context.Context, calls []chatToolCall, out chan<- Msg) ([]chatMessage, error) {
	functionCalls := make([]functionCall, 0, len(calls))
	for _, call := range calls {
		functionCalls = append(functionCalls, functionCall{name: call.Name, callID: call.ID, rawArgs: call.Arguments})
	}
	toolCalls, err := buildToolCalls(functionCalls, o.toolMap)
	if err != nil {
		return nil, err
	}
	for _, call := range toolCalls {
		out <- Msg{
			Type:     MsgTypeToolCall,
			Value:    call.raw,
			ToolCall: &ToolCallInfo{Name: call.name, CallID: call.callID},
		}
	}

	runner := o.toolRunner
	if runner == nil {
		runner = parallelToolRunner{}
	}
	var (
		mu          sync.Mutex
		resultsByID = make(map[string]toolResult, len(toolCalls))
	)
	results := runner.Run(ctx, toolCalls, func(call toolCall, result toolResult) {
		if call.callID != "" {
			mu.Lock()
			resultsByID[call.callID] = result
			mu.Unlock()
		}
		if result.err != nil {
			return
		}
		out <- Msg{
			Type:     MsgTypeToolResult,
			Value:    result.output,
			ToolCall: &ToolCallInfo{Name: call.name, CallID: call.callID},
		}
	})

	messages := make([]chatMessage, 0, len(toolCalls))
	for i, call := range toolCalls {
		result, ok := resultsByID[call.callID]
		if !ok && i < len(results) {
			result = results[i]
		}
		if result.err != nil {
			return nil, result.err
		}
		messages = append(messages, chatMessage{
			Role:       roleTool,
			Content:    result.output,
			ToolCallID: call.callID,
		})
	}
	return messages, nil
}

func (o *OpenRouter) buildParams() openai.ChatCompletionNewParams {
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(o.model),
		Messages: o.chatMessagesToParams(o.state.snapshot()),
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	}
	if o.params.ReasoningEffort != "" {
		params.ReasoningEffort = o.params.ReasoningEffort
	}
	if len(o.toolDefs) > 0 {
		params.Tools = o.toolDefs
	}
	return params
}

func (o *OpenRouter) chatMessagesToParams(messages []chatMessage) []openai.ChatCompletionMessageParamUnion {
	params := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, message := range messages {
		params = append(params, chatMessageToParam(message))
	}
	return params
}

func chatMessageToParam(message chatMessage) openai.ChatCompletionMessageParamUnion {
	switch message.Role {
	case roleSystem:
		return openai.SystemMessage(message.Content)
	case roleTool:
		return openai.ToolMessage(message.Content, message.ToolCallID)
	case roleAssistant:
		return assistantMessageToParam(message)
	default:
		return openai.UserMessage(message.Content)
	}
}

func assistantMessageToParam(message chatMessage) openai.ChatCompletionMessageParamUnion {
	assistant := openai.ChatCompletionAssistantMessageParam{}
	if message.Content != "" {
		assistant.Content.OfString = openai.String(message.Content)
	}
	if len(message.ToolCalls) > 0 {
		toolCalls := make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: call.ID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      call.Name,
						Arguments: call.Arguments,
					},
				},
			})
		}
		assistant.ToolCalls = toolCalls
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant}
}

func emitFinalChatMessages(out chan<- Msg, content string, reasoning string, final *FinalInfo) {
	emitFinalMessage(out, MsgTypeChatFinal, content, "", final)
	emitFinalMessage(out, MsgTypeReasoningSummaryFinal, reasoning, "", nil)
}

func (o *OpenRouter) finalMessageInfo(result chatCompletionResult) *FinalInfo {
	if !result.hasUsage || o.maxContextTokens <= 0 {
		return nil
	}
	remaining := o.maxContextTokens - result.usage.PromptTokens
	if remaining < 0 {
		remaining = 0
	}
	return &FinalInfo{RemainingTokens: &remaining}
}

func (o *OpenRouter) emitContextUsage(result chatCompletionResult, out chan<- Msg) {
	if !result.hasUsage {
		return
	}
	used := result.usage.PromptTokens
	if used < 0 {
		used = 0
	}
	usage := newContextUsage(used, o.maxContextTokens, result.usage.CompletionTokens, result.usage.TotalTokens)
	value := ""
	if o.maxContextTokens > 0 {
		value = fmt.Sprintf("%.1f%%", usage.PercentUsed)
	}
	out <- Msg{Type: MsgTypeContextUsage, Value: value, Usage: usage}
}

func (o *OpenRouter) PerformCompression(ctx context.Context, sessionID string, compressor Compressor) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context is required")
	}
	if compressor == nil {
		return "", fmt.Errorf("compressor is required")
	}

	_ = sessionID
	sessionID = o.sessionID
	beforeUsage := compressionUsageSnapshot{}
	captureProvider := compressionUsageCaptureProvider{provider: o, onContextUsage: beforeUsage.capture}
	previousMessages := o.state.snapshot()
	compressed, err := compressor.Compress(ctx, captureProvider, sessionID)
	if err != nil {
		o.state.replace(previousMessages)
		return "", err
	}

	o.state.replace(nil)
	afterUsage, err := o.seedCompressedContext(ctx, sessionID, compressed)
	if err != nil {
		o.state.replace(previousMessages)
		return "", err
	}
	if statusMsg, ok := compressionStatusMsg(beforeUsage, afterUsage); ok {
		SetCompressionStatus(ctx, statusMsg)
	}
	return compressed, nil
}

func (o *OpenRouter) seedCompressedContext(ctx context.Context, sessionID string, compressed string) (compressionUsageSnapshot, error) {
	_ = sessionID
	usage := compressionUsageSnapshot{}
	compressed = strings.TrimSpace(compressed)
	if compressed == "" {
		return usage, fmt.Errorf("compressed context is empty")
	}
	stream := o.Chat(ctx, compressionSeedPromptPrefix+compressed)
	if stream == nil {
		return usage, fmt.Errorf("provider returned nil stream while injecting compressed context")
	}
	for msg := range stream {
		usage.capture(msg)
		if msg.Type != MsgTypeError {
			continue
		}
		errText := strings.TrimSpace(msg.Value)
		if errText == "" {
			errText = "provider returned an empty error"
		}
		return usage, fmt.Errorf("compression injection failed: %s", errText)
	}
	if err := ctx.Err(); err != nil {
		return usage, err
	}
	return usage, nil
}
