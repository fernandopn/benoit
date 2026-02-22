package session

import (
	"context"

	"github.com/fernandopn/benoit/providers"
	"github.com/fernandopn/benoit/tools"
)

type PreviousResponseLookup interface {
	PreviousResponseID(ctx context.Context, providerType providers.ProviderType, sessionID string) (string, bool, error)
}

type MiddlewareFactory func(ctx context.Context, provider providers.Provider, providerType providers.ProviderType, sessionID string) (providers.Provider, error)

type ProviderBuilder func(ctx context.Context, model string, apiKey string, params providers.OpenAIProviderParams, toolSet []tools.Tool) (providers.Provider, func() error, error)

func DefaultProviderBuilder(ctx context.Context, model string, apiKey string, params providers.OpenAIProviderParams, toolSet []tools.Tool) (providers.Provider, func() error, error) {
	_ = ctx
	provider, err := providers.NewOpenAI(model, apiKey, params, toolSet)
	if err != nil {
		return nil, nil, err
	}
	return provider, nil, nil
}
