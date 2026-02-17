package providers

import (
	"context"
	"strings"
	"sync"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// StreamingOpenAI uses the Responses streaming API.
type StreamingOpenAI struct {
	client openai.Client
	state  *openAIState
}

// DirectOpenAI uses the non-streaming Responses API.
type DirectOpenAI struct {
	client openai.Client
	state  *openAIState
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

func NewStreamingOpenAI(client openai.Client) *StreamingOpenAI {
	return &StreamingOpenAI{client: client, state: &openAIState{}}
}

func NewDirectOpenAI(client openai.Client) *DirectOpenAI {
	return &DirectOpenAI{client: client, state: &openAIState{}}
}

func buildParams(input string, previousID string) responses.ResponseNewParams {
	params := responses.ResponseNewParams{
		Model: openai.ChatModelGPT5_2,
		Input: responses.ResponseNewParamsInputUnion{OfString: openai.String(input)},
	}
	if previousID != "" {
		params.PreviousResponseID = openai.String(previousID)
	}
	return params
}

func (s *StreamingOpenAI) Chat(ctx context.Context, input string) <-chan Msg {
	out := make(chan Msg)
	params := buildParams(input, s.state.get())
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
	params := buildParams(input, d.state.get())

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
