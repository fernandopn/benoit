package providers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

type baseOpenAI struct {
	client openai.Client
	state  *openAIState
	model  string
	kind   string
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

func NewStreamingOpenAI(ctx context.Context, model string) (*StreamingOpenAI, error) {
	base, err := newBaseOpenAI(ctx, "StreamingOpenAI", model)
	if err != nil {
		return nil, err
	}
	return &StreamingOpenAI{baseOpenAI: base}, nil
}

func NewDirectOpenAI(ctx context.Context, model string) (*DirectOpenAI, error) {
	base, err := newBaseOpenAI(ctx, "DirectOpenAI", model)
	if err != nil {
		return nil, err
	}
	return &DirectOpenAI{baseOpenAI: base}, nil
}

func newBaseOpenAI(ctx context.Context, kind string, model string) (*baseOpenAI, error) {
	if _, ok := os.LookupEnv("OPENAI_API_KEY"); !ok {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set")
	}

	base := &baseOpenAI{client: openai.NewClient(), state: &openAIState{}, kind: kind}
	resolved, err := base.resolveModel(ctx, model)
	if err != nil {
		return nil, err
	}
	base.model = resolved
	return base, nil
}

func (b *baseOpenAI) buildParams(input string, previousID string) responses.ResponseNewParams {
	params := responses.ResponseNewParams{
		Model: openai.ChatModel(b.model),
		Input: responses.ResponseNewParamsInputUnion{OfString: openai.String(input)},
	}
	if previousID != "" {
		params.PreviousResponseID = openai.String(previousID)
	}
	return params
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
	page, err := b.client.Models.List(ctx)
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

func (b *baseOpenAI) Name() string {
	return fmt.Sprintf("%s (%s)", b.kind, b.model)
}

func modelInList(models []string, value string) bool {
	for _, model := range models {
		if model == value {
			return true
		}
	}
	return false
}

func (s *StreamingOpenAI) Chat(ctx context.Context, input string) <-chan Msg {
	out := make(chan Msg)
	params := s.buildParams(input, s.state.get())
	stream := s.client.Responses.NewStreaming(ctx, params)

	go func() {
		defer close(out)

		for stream.Next() {
			event := stream.Current()
			if event.Type == "response.output_text.delta" && event.Delta != "" {
				out <- Msg{Type: MsgTypeChat, Value: event.Delta}
			}
			if event.Type == "response.completed" && event.Response.ID != "" {
				s.state.set(event.Response.ID)
			}
		}

		if err := stream.Err(); err != nil {
			out <- Msg{Type: MsgTypeError, Value: err.Error()}
			return
		}

	}()

	return out
}

func (d *DirectOpenAI) Chat(ctx context.Context, input string) <-chan Msg {
	out := make(chan Msg, 1)
	params := d.buildParams(input, d.state.get())

	go func() {
		defer close(out)

		resp, err := d.client.Responses.New(ctx, params)
		if err != nil {
			out <- Msg{Type: MsgTypeError, Value: err.Error()}
			return
		}

		output := strings.TrimSpace(resp.OutputText())
		if output != "" {
			out <- Msg{Type: MsgTypeChat, Value: output}
		}

		if resp.ID != "" {
			d.state.set(resp.ID)
		}
	}()

	return out
}
