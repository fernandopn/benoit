package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/fernandopn/benoit/tools"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// === Abstractions for testing ===

type responseStream interface {
	Next() bool
	Current() responses.ResponseStreamEventUnion
	Err() error
}

type openAIClient interface {
	ListModels(ctx context.Context) ([]string, error)
	NewStreamingResponse(ctx context.Context, params responses.ResponseNewParams) responseStream
}

type openAIClientAdapter struct {
	client openai.Client
}

func newOpenAIClientAdapter(apiKey string) *openAIClientAdapter {
	return &openAIClientAdapter{client: openai.NewClient(option.WithAPIKey(apiKey))}
}

func (a *openAIClientAdapter) ListModels(ctx context.Context) ([]string, error) {
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

func (a *openAIClientAdapter) NewStreamingResponse(ctx context.Context, params responses.ResponseNewParams) responseStream {
	return a.client.Responses.NewStreaming(ctx, params)
}

const toolBatchingInstruction = "When tool calls are independent, emit all needed tool calls in a single response (parallel tool calls). After receiving a directory listing, batch list_files calls for all subdirectories in one response. Do not serialize independent tool calls."
const compressionSeedPromptPrefix = "Treat the following compressed context as authoritative memory for future turns. Do not call tools. Reply with exactly OK.\n\nCompressed context:\n"

// OpenAI uses the Responses streaming API.
type OpenAI struct {
	client           openAIClient
	state            *openAIState
	model            string
	maxContextTokens int64
	params           OpenAIParams
	toolDefs         []responses.ToolUnionParam
	toolMap          map[string]tools.Tool
	toolRunner       toolRunner
}

type OpenAIParams struct {
	ReasoningEffort  shared.ReasoningEffort
	ReasoningSummary shared.ReasoningSummary
}

const defaultOpenAISessionID = "__default__"

type openAIState struct {
	mu                sync.Mutex
	previousBySession map[string]string
}

func newOpenAIState() *openAIState {
	return &openAIState{previousBySession: map[string]string{}}
}

func normalizeSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return defaultOpenAISessionID
	}
	return sessionID
}

func (s *openAIState) get(sessionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.previousBySession == nil {
		return ""
	}
	return s.previousBySession[normalizeSessionID(sessionID)]
}

func (s *openAIState) set(sessionID string, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.previousBySession == nil {
		s.previousBySession = map[string]string{}
	}
	s.previousBySession[normalizeSessionID(sessionID)] = id
}

func (s *openAIState) reset(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.previousBySession == nil {
		return
	}
	delete(s.previousBySession, normalizeSessionID(sessionID))
}

func newOpenAI(model string, apiKey string, params OpenAIParams, toolSet []tools.Tool) (*OpenAI, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("api key is required")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("model is required")
	}

	provider := &OpenAI{
		client:     newOpenAIClientAdapter(apiKey),
		state:      newOpenAIState(),
		model:      model,
		toolRunner: parallelToolRunner{},
		params:     params,
	}
	provider.maxContextTokens = provider.contextTokensForModel(provider.model)
	if err := provider.initTools(toolSet); err != nil {
		return nil, err
	}
	return provider, nil
}

func (b *OpenAI) buildParamsWithInput(input responses.ResponseNewParamsInputUnion, previousID string) responses.ResponseNewParams {
	params := responses.ResponseNewParams{
		Model: openai.ChatModel(b.model),
		Input: input,
	}
	params.Instructions = openai.String(toolBatchingInstruction)
	if previousID != "" {
		params.PreviousResponseID = openai.String(previousID)
	}
	if b.params.ReasoningEffort != "" || b.params.ReasoningSummary != "" {
		params.Reasoning = shared.ReasoningParam{
			Effort:  b.params.ReasoningEffort,
			Summary: b.params.ReasoningSummary,
		}
	}
	params.ParallelToolCalls = openai.Bool(true)
	if len(b.toolDefs) > 0 {
		params.Tools = b.toolDefs
	}
	return params
}

func (b *OpenAI) buildParams(input string, previousID string) responses.ResponseNewParams {
	return b.buildParamsWithInput(
		responses.ResponseNewParamsInputUnion{OfString: openai.String(input)},
		previousID,
	)
}

func (b *OpenAI) buildToolParams(previousID string, input responses.ResponseInputParam) responses.ResponseNewParams {
	return b.buildParamsWithInput(
		responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		previousID,
	)
}

func (b *OpenAI) initTools(toolSet []tools.Tool) error {
	if len(toolSet) == 0 {
		return nil
	}
	b.toolDefs = make([]responses.ToolUnionParam, 0, len(toolSet))
	b.toolMap = make(map[string]tools.Tool, len(toolSet))
	for _, tool := range toolSet {
		if tool == nil {
			continue
		}
		name := strings.TrimSpace(tool.Name())
		if name == "" {
			return fmt.Errorf("tool name cannot be empty")
		}
		if _, exists := b.toolMap[name]; exists {
			return fmt.Errorf("duplicate tool name: %s", name)
		}
		b.toolMap[name] = tool
		b.toolDefs = append(b.toolDefs, tool.Definition())
	}
	return nil
}

func (b *OpenAI) ListModels(ctx context.Context) ([]string, error) {
	return b.client.ListModels(ctx)
}

func (b *OpenAI) Name() string {
	return fmt.Sprintf("OpenAI %s", b.model)
}

type compressionUsageSnapshot struct {
	usedTokens      int64
	availableTokens int64
	hasValue        bool
}

func (s *compressionUsageSnapshot) capture(msg Msg) {
	if msg.Type != MsgTypeContextUsage || msg.Metadata == nil {
		return
	}
	usedRaw := strings.TrimSpace(msg.Metadata["tokens_input_used"])
	if usedRaw == "" {
		usedRaw = strings.TrimSpace(msg.Metadata["tokens_used"])
	}
	availableRaw := strings.TrimSpace(msg.Metadata["tokens_available"])
	used, usedOK := parseInt64Loose(usedRaw)
	available, availableOK := parseInt64Loose(availableRaw)
	if !usedOK || !availableOK || available <= 0 || used < 0 {
		return
	}
	s.usedTokens = used
	s.availableTokens = available
	s.hasValue = true
}

type compressionUsageCaptureProvider struct {
	provider       Provider
	onContextUsage func(Msg)
}

func (p compressionUsageCaptureProvider) Chat(ctx context.Context, input string) <-chan Msg {
	return forwardStreamWithUsageCapture(p.provider.Chat(ctx, input), p.onContextUsage)
}

func (p compressionUsageCaptureProvider) PerformCompression(ctx context.Context, sessionID string, compressor Compressor) (string, error) {
	return p.provider.PerformCompression(ctx, sessionID, compressor)
}

func (p compressionUsageCaptureProvider) ListModels(ctx context.Context) ([]string, error) {
	return p.provider.ListModels(ctx)
}

func (p compressionUsageCaptureProvider) Name() string {
	return p.provider.Name()
}

func (p compressionUsageCaptureProvider) ChatInSession(ctx context.Context, input string, sessionID string) <-chan Msg {
	sessionProvider, ok := p.provider.(SessionProvider)
	if !ok {
		return p.Chat(ctx, input)
	}
	return forwardStreamWithUsageCapture(sessionProvider.ChatInSession(ctx, input, sessionID), p.onContextUsage)
}

func forwardStreamWithUsageCapture(in <-chan Msg, hook func(Msg)) <-chan Msg {
	if in == nil {
		return nil
	}
	out := make(chan Msg)
	go func() {
		defer close(out)
		for msg := range in {
			if hook != nil {
				hook(msg)
			}
			out <- msg
		}
	}()
	return out
}

func compressionStatusMsg(before compressionUsageSnapshot, after compressionUsageSnapshot) (Msg, bool) {
	if !before.hasValue || !after.hasValue {
		return Msg{}, false
	}
	beforeLeft, beforeOK := contextLeftPercent(before.usedTokens, before.availableTokens)
	afterLeft, afterOK := contextLeftPercent(after.usedTokens, after.availableTokens)
	if !beforeOK || !afterOK {
		return Msg{}, false
	}
	return Msg{
		Type: MsgTypeCompressionStatus,
		Value: fmt.Sprintf(
			"Context compressed from %d (%.1f%% left) to %d (%.1f%% left).",
			before.usedTokens,
			beforeLeft,
			after.usedTokens,
			afterLeft,
		),
		Metadata: map[string]string{
			"from_tokens_used":      strconv.FormatInt(before.usedTokens, 10),
			"from_tokens_available": strconv.FormatInt(before.availableTokens, 10),
			"from_left_percent":     fmt.Sprintf("%.1f", beforeLeft),
			"to_tokens_used":        strconv.FormatInt(after.usedTokens, 10),
			"to_tokens_available":   strconv.FormatInt(after.availableTokens, 10),
			"to_left_percent":       fmt.Sprintf("%.1f", afterLeft),
		},
	}, true
}

func (s *OpenAI) PerformCompression(ctx context.Context, sessionID string, compressor Compressor) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context is required")
	}
	if compressor == nil {
		return "", fmt.Errorf("compressor is required")
	}

	sessionID = normalizeSessionID(sessionID)
	beforeUsage := compressionUsageSnapshot{}
	captureProvider := compressionUsageCaptureProvider{provider: s, onContextUsage: beforeUsage.capture}
	previousID := s.state.get(sessionID)
	compressed, err := compressor.Compress(ctx, captureProvider, sessionID)
	if err != nil {
		s.state.reset(sessionID)
		if previousID != "" {
			s.state.set(sessionID, previousID)
		}
		return "", err
	}

	s.state.reset(sessionID)
	afterUsage, err := s.seedCompressedContext(ctx, sessionID, compressed)
	if err != nil {
		s.state.reset(sessionID)
		if previousID != "" {
			s.state.set(sessionID, previousID)
		}
		return "", err
	}
	if statusMsg, ok := compressionStatusMsg(beforeUsage, afterUsage); ok {
		SetCompressionStatus(ctx, statusMsg)
	}
	return compressed, nil
}

func (s *OpenAI) seedCompressedContext(ctx context.Context, sessionID string, compressed string) (compressionUsageSnapshot, error) {
	usage := compressionUsageSnapshot{}
	compressed = strings.TrimSpace(compressed)
	if compressed == "" {
		return usage, fmt.Errorf("compressed context is empty")
	}
	seedPrompt := compressionSeedPromptPrefix + compressed
	stream := s.ChatInSession(ctx, seedPrompt, sessionID)
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

func (b *OpenAI) toolOutputsFromResponse(ctx context.Context, resp *responses.Response, out chan<- Msg) (responses.ResponseInputParam, error) {
	calls := functionCallsFromResponse(resp)
	if len(calls) == 0 {
		return nil, nil
	}
	toolCalls, err := buildToolCalls(calls, b.toolMap)
	if err != nil {
		return nil, err
	}
	for _, call := range toolCalls {
		out <- Msg{
			Type:  MsgTypeToolCall,
			Value: call.raw,
			Metadata: map[string]string{
				"tool":    call.name,
				"call_id": call.callID,
			},
		}
	}

	runner := b.toolRunner
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
			Type:  MsgTypeToolResult,
			Value: result.output,
			Metadata: map[string]string{
				"tool":    call.name,
				"call_id": call.callID,
			},
		}
	})
	outputs := make(responses.ResponseInputParam, 0, len(toolCalls))
	for i, call := range toolCalls {
		result, ok := resultsByID[call.callID]
		if !ok && i < len(results) {
			result = results[i]
		}
		if result.err != nil {
			return nil, result.err
		}
		outputs = append(outputs, responses.ResponseInputItemParamOfFunctionCallOutput(call.callID, result.output))
	}
	return outputs, nil
}

func parseToolArgs(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, err
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}

func (b *OpenAI) contextTokensForModel(model string) int64 {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(model, "gpt-5.2-chat"):
		return 128000
	case strings.HasPrefix(model, "gpt-5.2-codex"):
		return 400000
	case strings.HasPrefix(model, "gpt-5.2"):
		return 400000
	}
	return 0
}

func parseInt64Loose(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	value = strings.ReplaceAll(value, ",", "")
	num, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return num, true
}

func contextLeftPercent(usedTokens int64, availableTokens int64) (float64, bool) {
	if availableTokens <= 0 || usedTokens < 0 {
		return 0, false
	}
	left := ((float64(availableTokens) - float64(usedTokens)) / float64(availableTokens)) * 100
	if left < 0 {
		left = 0
	}
	if left > 100 {
		left = 100
	}
	return left, true
}

func (b *OpenAI) contextUsageMetrics(resp *responses.Response) (int64, int64, int64, int64, bool) {
	if resp == nil || !resp.JSON.Usage.Valid() {
		return 0, 0, 0, 0, false
	}
	available := b.maxContextTokens
	if available <= 0 {
		return 0, 0, 0, 0, false
	}
	used := resp.Usage.InputTokens
	if used < 0 {
		return 0, 0, 0, 0, false
	}
	output := resp.Usage.OutputTokens
	total := resp.Usage.TotalTokens
	return used, available, output, total, true
}

func (b *OpenAI) contextUsageMsg(resp *responses.Response) *Msg {
	used, available, output, total, ok := b.contextUsageMetrics(resp)
	if !ok {
		return nil
	}
	percentage := (float64(used) / float64(available)) * 100
	return &Msg{
		Type:  MsgTypeContextUsage,
		Value: fmt.Sprintf("%.1f%%", percentage),
		Metadata: map[string]string{
			"tokens_used":        strconv.FormatInt(used, 10),
			"tokens_input_used":  strconv.FormatInt(used, 10),
			"tokens_output_used": strconv.FormatInt(output, 10),
			"tokens_total_used":  strconv.FormatInt(total, 10),
			"tokens_available":   strconv.FormatInt(available, 10),
		},
	}
}

func (b *OpenAI) emitContextUsage(resp *responses.Response, out chan<- Msg) {
	if b.maxContextTokens <= 0 {
		return
	}
	if usage := b.contextUsageMsg(resp); usage != nil {
		out <- *usage
	}
}

func NewOpenAI(model string, apiKey string, params OpenAIParams, toolSet []tools.Tool) (*OpenAI, error) {
	provider, err := newOpenAI(model, apiKey, params, toolSet)
	if err != nil {
		return nil, err
	}
	return provider, nil
}

func (s *OpenAI) Chat(ctx context.Context, input string) <-chan Msg {
	return s.ChatInSession(ctx, input, "")
}

func (s *OpenAI) ChatInSession(ctx context.Context, input string, sessionID string) <-chan Msg {
	out := make(chan Msg)
	sessionID = normalizeSessionID(sessionID)

	go func() {
		defer close(out)

		params := s.buildParams(input, s.state.get(sessionID))
		for {
			response, err := s.streamResponse(ctx, params, out)
			if err != nil {
				out <- Msg{Type: MsgTypeError, Value: err.Error()}
				return
			}
			if response == nil {
				return
			}
			if response.ID != "" {
				s.state.set(sessionID, response.ID)
			}
			toolOutputs, err := s.toolOutputsFromResponse(ctx, response, out)
			if err != nil {
				out <- Msg{Type: MsgTypeError, Value: err.Error()}
				return
			}
			s.emitContextUsage(response, out)
			if len(toolOutputs) == 0 {
				return
			}
			params = s.buildToolParams(response.ID, toolOutputs)
		}
	}()

	return out
}

func (s *OpenAI) streamResponse(ctx context.Context, params responses.ResponseNewParams, out chan<- Msg) (*responses.Response, error) {
	stream := s.client.NewStreamingResponse(ctx, params)
	var (
		completed      *responses.Response
		chatDelta      strings.Builder
		reasoningDelta strings.Builder
	)

	for stream.Next() {
		event := stream.Current()
		if event.Type == "response.output_text.delta" && event.Delta != "" {
			out <- Msg{Type: MsgTypeChatDelta, Value: event.Delta}
			chatDelta.WriteString(event.Delta)
		}
		if event.Type == "response.reasoning_summary_text.delta" && event.Delta != "" {
			out <- Msg{Type: MsgTypeReasoningSummaryDelta, Value: event.Delta}
			reasoningDelta.WriteString(event.Delta)
		}
		if event.Type == "response.completed" {
			completed = &event.Response
		}
	}
	if err := stream.Err(); err != nil {
		return completed, err
	}

	emitFinalStreamMessages(out, completed, chatDelta.String(), reasoningDelta.String())
	return completed, nil
}

func emitFinalStreamMessages(out chan<- Msg, completed *responses.Response, chatDelta string, reasoningDelta string) {
	emitFinalMessage(out, MsgTypeChatFinal, responseChatText(completed), chatDelta)
	emitFinalMessage(out, MsgTypeReasoningSummaryFinal, responseReasoningSummaryText(completed), reasoningDelta)
}

func emitFinalMessage(out chan<- Msg, messageType MsgType, explicit string, fallback string) {
	value := explicit
	if strings.TrimSpace(value) == "" {
		value = fallback
	}
	if strings.TrimSpace(value) == "" {
		return
	}
	out <- Msg{Type: messageType, Value: value}
}

func responseChatText(resp *responses.Response) string {
	if resp == nil {
		return ""
	}
	return resp.OutputText()
}

func responseReasoningSummaryText(resp *responses.Response) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}

	var summary strings.Builder
	for _, item := range resp.Output {
		if item.Type != "reasoning" {
			continue
		}
		reasoning := item.AsReasoning()
		for _, part := range reasoning.Summary {
			summary.WriteString(part.Text)
		}
	}
	return summary.String()
}
