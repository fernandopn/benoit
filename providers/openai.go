package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/fernandopn/benoid/tools"
	"github.com/openai/openai-go/v3"
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
	NewResponse(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error)
	NewStreamingResponse(ctx context.Context, params responses.ResponseNewParams) responseStream
}

type openAIClientAdapter struct {
	client openai.Client
}

func newOpenAIClientAdapter() *openAIClientAdapter {
	return &openAIClientAdapter{client: openai.NewClient()}
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

func (a *openAIClientAdapter) NewResponse(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error) {
	return a.client.Responses.New(ctx, params)
}

func (a *openAIClientAdapter) NewStreamingResponse(ctx context.Context, params responses.ResponseNewParams) responseStream {
	return a.client.Responses.NewStreaming(ctx, params)
}

// === Generic OpenAI ===

const toolBatchingInstruction = "When tool calls are independent, emit all needed tool calls in a single response (parallel tool calls). After receiving a directory listing, batch list_files calls for all subdirectories in one response. Do not serialize independent tool calls."

type baseOpenAI struct {
	client           openAIClient
	state            *openAIState
	model            string
	kind             string
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

type openAIState struct {
	mu         sync.Mutex
	previousID string
}

func (s *openAIState) get() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.previousID
}

func (s *openAIState) set(id string) {
	s.mu.Lock()
	s.previousID = id
	s.mu.Unlock()
}

func newBaseOpenAI(ctx context.Context, kind string, model string, params OpenAIParams, toolSet []tools.Tool) (*baseOpenAI, error) {
	if _, ok := os.LookupEnv("OPENAI_API_KEY"); !ok {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set")
	}

	base := &baseOpenAI{
		client:     newOpenAIClientAdapter(),
		state:      &openAIState{},
		kind:       kind,
		toolRunner: parallelToolRunner{},
		params:     params,
	}
	resolved, err := base.resolveModel(ctx, model)
	if err != nil {
		return nil, err
	}
	base.model = resolved
	base.maxContextTokens = base.contextTokensForModel(base.model)
	if err := base.initTools(toolSet); err != nil {
		return nil, err
	}
	return base, nil
}

func (b *baseOpenAI) buildParamsWithInput(input responses.ResponseNewParamsInputUnion, previousID string) responses.ResponseNewParams {
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

func (b *baseOpenAI) buildParams(input string, previousID string) responses.ResponseNewParams {
	return b.buildParamsWithInput(
		responses.ResponseNewParamsInputUnion{OfString: openai.String(input)},
		previousID,
	)
}

func (b *baseOpenAI) buildToolParams(previousID string, input responses.ResponseInputParam) responses.ResponseNewParams {
	return b.buildParamsWithInput(
		responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		previousID,
	)
}

func (b *baseOpenAI) initTools(toolSet []tools.Tool) error {
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

func (b *baseOpenAI) resolveModel(ctx context.Context, model string) (string, error) {
	models, err := b.ListModels(ctx)
	if err != nil {
		return "", err
	}
	if modelInList(models, model) {
		return model, nil
	}
	return "", fmt.Errorf("model not supported: %s. Available models: %s", model, strings.Join(models, ", "))
}

func (b *baseOpenAI) ListModels(ctx context.Context) ([]string, error) {
	return b.client.ListModels(ctx)
}

func (b *baseOpenAI) Name() string {
	return fmt.Sprintf("%s %s", b.kind, b.model)
}

func (b *baseOpenAI) toolOutputsFromResponse(ctx context.Context, resp *responses.Response, out chan<- Msg) (responses.ResponseInputParam, error) {
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

func modelInList(models []string, value string) bool {
	for _, model := range models {
		if model == value {
			return true
		}
	}
	return false
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

func (b *baseOpenAI) contextTokensForModel(model string) int64 {
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

func (b *baseOpenAI) emitReasoningFromResponse(resp *responses.Response, out chan<- Msg) {
	if resp == nil {
		return
	}
	for _, item := range resp.Output {
		if item.Type != "reasoning" {
			continue
		}
		reasoning := item.AsReasoning()
		for _, summary := range reasoning.Summary {
			if summary.Text == "" {
				continue
			}
			out <- Msg{Type: MsgTypeReasoningSummary, Value: summary.Text}
		}
	}
}

func (b *baseOpenAI) contextUsageMsg(resp *responses.Response) *Msg {
	if resp == nil || !resp.JSON.Usage.Valid() {
		return nil
	}
	used := resp.Usage.TotalTokens
	if used <= 0 {
		return nil
	}
	available := b.maxContextTokens
	if available <= 0 {
		return nil
	}
	percentage := (float64(used) / float64(available)) * 100
	return &Msg{
		Type:  MsgTypeContextUsage,
		Value: fmt.Sprintf("%.1f%%", percentage),
		Metadata: map[string]string{
			"tokens_used":      strconv.FormatInt(used, 10),
			"tokens_available": strconv.FormatInt(available, 10),
		},
	}
}

func (b *baseOpenAI) emitContextUsage(resp *responses.Response, out chan<- Msg) {
	if b.maxContextTokens <= 0 {
		return
	}
	if usage := b.contextUsageMsg(resp); usage != nil {
		out <- *usage
	}
}

// === Streaming OpenAI ===

// StreamingOpenAI uses the Responses streaming API.
type StreamingOpenAI struct {
	*baseOpenAI
}

func NewStreamingOpenAI(ctx context.Context, model string, params OpenAIParams, toolSet []tools.Tool) (*StreamingOpenAI, error) {
	base, err := newBaseOpenAI(ctx, "StreamingOpenAI", model, params, toolSet)
	if err != nil {
		return nil, err
	}
	return &StreamingOpenAI{baseOpenAI: base}, nil
}

func (s *StreamingOpenAI) Chat(ctx context.Context, input string) <-chan Msg {
	out := make(chan Msg)

	go func() {
		defer close(out)

		params := s.buildParams(input, s.state.get())
		for {
			response, err := s.streamResponse(ctx, params, out)
			if err != nil {
				out <- Msg{Type: MsgTypeError, Value: err.Error()}
				return
			}
			if response == nil {
				return
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

func (s *StreamingOpenAI) streamResponse(ctx context.Context, params responses.ResponseNewParams, out chan<- Msg) (*responses.Response, error) {
	stream := s.client.NewStreamingResponse(ctx, params)
	var completed *responses.Response
	for stream.Next() {
		event := stream.Current()
		if event.Type == "response.output_text.delta" && event.Delta != "" {
			out <- Msg{Type: MsgTypeChat, Value: event.Delta}
		}
		if event.Type == "response.reasoning_summary_text.delta" && event.Delta != "" {
			out <- Msg{Type: MsgTypeReasoningSummary, Value: event.Delta}
		}
		if event.Type == "response.completed" {
			completed = &event.Response
			if event.Response.ID != "" {
				s.state.set(event.Response.ID)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return completed, err
	}
	return completed, nil
}

// === Direct OpenAI ===

// DirectOpenAI uses the non-streaming Responses API.
type DirectOpenAI struct {
	*baseOpenAI
}

func NewDirectOpenAI(ctx context.Context, model string, params OpenAIParams, toolSet []tools.Tool) (*DirectOpenAI, error) {
	base, err := newBaseOpenAI(ctx, "DirectOpenAI", model, params, toolSet)
	if err != nil {
		return nil, err
	}
	return &DirectOpenAI{baseOpenAI: base}, nil
}

func (d *DirectOpenAI) Chat(ctx context.Context, input string) <-chan Msg {
	out := make(chan Msg, 1)

	go func() {
		defer close(out)

		params := d.buildParams(input, d.state.get())
		for {
			resp, err := d.client.NewResponse(ctx, params)
			if err != nil {
				out <- Msg{Type: MsgTypeError, Value: err.Error()}
				return
			}
			if resp.ID != "" {
				d.state.set(resp.ID)
			}

			toolOutputs, err := d.toolOutputsFromResponse(ctx, resp, out)
			if err != nil {
				out <- Msg{Type: MsgTypeError, Value: err.Error()}
				return
			}
			d.emitReasoningFromResponse(resp, out)
			if len(toolOutputs) == 0 {
				output := strings.TrimSpace(resp.OutputText())
				if output != "" {
					out <- Msg{Type: MsgTypeChat, Value: output}
				}
				d.emitContextUsage(resp, out)
				return
			}
			d.emitContextUsage(resp, out)
			params = d.buildToolParams(resp.ID, toolOutputs)
		}
	}()

	return out
}
