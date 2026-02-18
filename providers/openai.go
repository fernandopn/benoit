package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/fernandopn/benoid/tools"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

const toolBatchingInstruction = "When tool calls are independent, emit all needed tool calls in a single response (parallel tool calls). After receiving a directory listing, batch list_files calls for all subdirectories in one response. Do not serialize independent tool calls."

type baseOpenAI struct {
	client     openAIClient
	state      *openAIState
	model      string
	kind       string
	toolDefs   []responses.ToolUnionParam
	toolMap    map[string]tools.Tool
	toolRunner toolRunner
}

// StreamingOpenAI uses the Responses streaming API.
type StreamingOpenAI struct {
	*baseOpenAI
}

// DirectOpenAI uses the non-streaming Responses API.
type DirectOpenAI struct {
	*baseOpenAI
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

func NewStreamingOpenAI(ctx context.Context, model string, toolSet []tools.Tool) (*StreamingOpenAI, error) {
	base, err := newBaseOpenAI(ctx, "StreamingOpenAI", model, toolSet)
	if err != nil {
		return nil, err
	}
	return &StreamingOpenAI{baseOpenAI: base}, nil
}

func NewDirectOpenAI(ctx context.Context, model string, toolSet []tools.Tool) (*DirectOpenAI, error) {
	base, err := newBaseOpenAI(ctx, "DirectOpenAI", model, toolSet)
	if err != nil {
		return nil, err
	}
	return &DirectOpenAI{baseOpenAI: base}, nil
}

func newBaseOpenAI(ctx context.Context, kind string, model string, toolSet []tools.Tool) (*baseOpenAI, error) {
	if _, ok := os.LookupEnv("OPENAI_API_KEY"); !ok {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set")
	}

	base := &baseOpenAI{
		client:     newOpenAIClientAdapter(),
		state:      &openAIState{},
		kind:       kind,
		toolRunner: parallelToolRunner{},
	}
	resolved, err := base.resolveModel(ctx, model)
	if err != nil {
		return nil, err
	}
	base.model = resolved
	if err := base.initTools(toolSet); err != nil {
		return nil, err
	}
	return base, nil
}

func (b *baseOpenAI) buildParams(input string, previousID string) responses.ResponseNewParams {
	params := responses.ResponseNewParams{
		Model: openai.ChatModel(b.model),
		Input: responses.ResponseNewParamsInputUnion{OfString: openai.String(input)},
	}
	params.Instructions = openai.String(toolBatchingInstruction)
	if previousID != "" {
		params.PreviousResponseID = openai.String(previousID)
	}
	params.ParallelToolCalls = openai.Bool(true)
	if len(b.toolDefs) > 0 {
		params.Tools = b.toolDefs
	}
	return params
}

func (b *baseOpenAI) buildToolParams(previousID string, input responses.ResponseInputParam) responses.ResponseNewParams {
	params := responses.ResponseNewParams{
		Model: openai.ChatModel(b.model),
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
	}
	params.Instructions = openai.String(toolBatchingInstruction)
	if previousID != "" {
		params.PreviousResponseID = openai.String(previousID)
	}
	params.ParallelToolCalls = openai.Bool(true)
	if len(b.toolDefs) > 0 {
		params.Tools = b.toolDefs
	}
	return params
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
	results := runner.Run(ctx, toolCalls)
	outputs := make(responses.ResponseInputParam, 0, len(toolCalls))
	for i, call := range toolCalls {
		result := results[i]
		if result.err != nil {
			return nil, result.err
		}
		out <- Msg{
			Type:  MsgTypeToolResult,
			Value: result.output,
			Metadata: map[string]string{
				"tool":    call.name,
				"call_id": call.callID,
			},
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
			if len(toolOutputs) == 0 {
				return
			}
			params = s.buildToolParams(response.ID, toolOutputs)
		}
	}()

	return out
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
			if len(toolOutputs) == 0 {
				output := strings.TrimSpace(resp.OutputText())
				if output != "" {
					out <- Msg{Type: MsgTypeChat, Value: output}
				}
				return
			}
			params = d.buildToolParams(resp.ID, toolOutputs)
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
