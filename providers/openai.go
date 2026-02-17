package providers

import (
	"bufio"
	"context"
	"io"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// StreamingOpenAI uses the Responses streaming API.
type StreamingOpenAI struct {
	client openai.Client
}

// DirectOpenAI uses the non-streaming Responses API.
type DirectOpenAI struct {
	client openai.Client
}

func NewStreamingOpenAI(client openai.Client) *StreamingOpenAI {
	return &StreamingOpenAI{client: client}
}

func NewDirectOpenAI(client openai.Client) *DirectOpenAI {
	return &DirectOpenAI{client: client}
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

func (s *StreamingOpenAI) Chat(ctx context.Context, input string, previousID string, w io.Writer) (string, error) {
	params := buildParams(input, previousID)
	stream := s.client.Responses.NewStreaming(ctx, params)

	writer := bufio.NewWriter(w)
	var (
		sawText     bool
		completedID string
	)

	for stream.Next() {
		event := stream.Current()
		if event.Type == "response.output_text.delta" && event.Delta != "" {
			_, _ = writer.WriteString(event.Delta)
			_ = writer.Flush()
			sawText = true
		}
		if event.Type == "response.completed" && event.Response.ID != "" {
			completedID = event.Response.ID
		}
	}

	if err := stream.Err(); err != nil {
		_ = writer.Flush()
		return previousID, err
	}

	if !sawText {
		_, _ = writer.WriteString("(no content)")
		_ = writer.Flush()
	}

	if completedID != "" {
		return completedID, nil
	}

	return previousID, nil
}

func (d *DirectOpenAI) Chat(ctx context.Context, input string, previousID string, w io.Writer) (string, error) {
	params := buildParams(input, previousID)
	resp, err := d.client.Responses.New(ctx, params)
	if err != nil {
		return previousID, err
	}

	output := strings.TrimSpace(resp.OutputText())
	if output == "" {
		output = "(no content)"
	}

	if _, err := io.WriteString(w, output); err != nil {
		return resp.ID, err
	}

	return resp.ID, nil
}
