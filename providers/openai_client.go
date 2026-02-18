package providers

import (
	"context"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

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
