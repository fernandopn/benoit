package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/fernandopn/benoit/providers"
	"github.com/fernandopn/benoit/tools"
)

type Config struct {
	Model                string
	OpenAIAPIKey         string
	OpenAIProviderParams providers.OpenAIProviderParams
	SessionLookup        PreviousResponseLookup
	MiddlewareFactories  []MiddlewareFactory
	ProviderBuilder      ProviderBuilder
}

type sessionProviderEntry struct {
	provider providers.Provider
	closeFn  func() error
}

type providerFactory struct {
	ctx           context.Context
	cfg           Config
	toolSet       []tools.Tool
	sessionLookup PreviousResponseLookup
	middleware    []MiddlewareFactory
	providerFn    ProviderBuilder

	mu       sync.Mutex
	entries  map[string]sessionProviderEntry
	provider providers.ProviderType
}

func NewRouterProvider(ctx context.Context, cfg Config, toolSet []tools.Tool) (providers.Provider, func() error, error) {
	if ctx == nil {
		return nil, nil, fmt.Errorf("context is required")
	}
	factory := newProviderFactory(ctx, cfg, toolSet, cfg.SessionLookup)
	router, err := newRouterProvider(factory)
	if err != nil {
		return nil, nil, err
	}
	return router, factory.Close, nil
}

func newProviderFactory(ctx context.Context, cfg Config, toolSet []tools.Tool, sessionLookup PreviousResponseLookup) *providerFactory {
	providerFn := cfg.ProviderBuilder
	if providerFn == nil {
		providerFn = DefaultProviderBuilder
	}
	middlewares := make([]MiddlewareFactory, 0, len(cfg.MiddlewareFactories))
	middlewares = append(middlewares, cfg.MiddlewareFactories...)
	return &providerFactory{
		ctx:           ctx,
		cfg:           cfg,
		toolSet:       toolSet,
		sessionLookup: sessionLookup,
		middleware:    middlewares,
		providerFn:    providerFn,
		entries:       map[string]sessionProviderEntry{},
		provider:      providers.ProviderTypeOpenAI,
	}
}

func (f *providerFactory) Name() string {
	model := strings.TrimSpace(f.cfg.Model)
	if model == "" {
		return "OpenAI"
	}
	return "OpenAI " + model
}

func (f *providerFactory) providerForSession(sessionID string) (providers.Provider, error) {
	normalizedSessionID := NormalizeSessionID(sessionID)

	f.mu.Lock()
	entry, ok := f.entries[normalizedSessionID]
	f.mu.Unlock()
	if ok && entry.provider != nil {
		return entry.provider, nil
	}

	provider, closeFn, err := f.createProvider(normalizedSessionID)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, exists := f.entries[normalizedSessionID]; exists && existing.provider != nil {
		if closeFn != nil {
			_ = closeFn()
		}
		return existing.provider, nil
	}
	f.entries[normalizedSessionID] = sessionProviderEntry{provider: provider, closeFn: closeFn}
	return provider, nil
}

func (f *providerFactory) createProvider(sessionID string) (providers.Provider, func() error, error) {
	params := f.cfg.OpenAIProviderParams
	params.SessionID = sessionID
	if f.sessionLookup != nil {
		previousID, found, err := f.sessionLookup.PreviousResponseID(f.ctx, f.provider, sessionID)
		if err != nil {
			return nil, nil, err
		}
		if found {
			params.PreviousResponseID = strings.TrimSpace(previousID)
		}
	}

	provider, closeFn, err := f.providerFn(f.ctx, f.cfg.Model, f.cfg.OpenAIAPIKey, params, f.toolSet)
	if err != nil {
		return nil, nil, err
	}

	var wrapped providers.Provider = provider
	for _, middlewareFactory := range f.middleware {
		wrapped, err = middlewareFactory(f.ctx, wrapped, f.provider, sessionID)
		if err != nil {
			if closeFn != nil {
				_ = closeFn()
			}
			return nil, nil, err
		}
	}
	return wrapped, closeFn, nil
}

func (f *providerFactory) Close() error {
	f.mu.Lock()
	entries := f.entries
	f.entries = map[string]sessionProviderEntry{}
	f.mu.Unlock()

	var closeErr error
	for _, entry := range entries {
		if entry.closeFn == nil {
			continue
		}
		if err := entry.closeFn(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	return closeErr
}

type routerProvider struct {
	factory          *providerFactory
	defaultSessionID string
}

var _ providers.Provider = (*routerProvider)(nil)

func newRouterProvider(factory *providerFactory) (providers.Provider, error) {
	if factory == nil {
		return nil, fmt.Errorf("provider factory is required")
	}
	return &routerProvider{factory: factory, defaultSessionID: NormalizeSessionID("")}, nil
}

func (r *routerProvider) Chat(ctx context.Context, input string) <-chan providers.Msg {
	if ctx == nil {
		return providerErrorStream(errors.New("context is required"))
	}
	sessionID := r.resolveSessionID(ctx, "")
	provider, err := r.factory.providerForSession(sessionID)
	if err != nil {
		return providerErrorStream(err)
	}
	return provider.Chat(providers.WithSessionID(ctx, sessionID), input)
}

func (r *routerProvider) PerformCompression(ctx context.Context, sessionID string, compressor providers.Compressor) (string, error) {
	if ctx == nil {
		return "", errors.New("context is required")
	}
	sessionID = r.resolveSessionID(ctx, sessionID)
	provider, err := r.factory.providerForSession(sessionID)
	if err != nil {
		return "", err
	}
	return provider.PerformCompression(providers.WithSessionID(ctx, sessionID), sessionID, compressor)
}

func (r *routerProvider) ListModels(ctx context.Context) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	provider, err := r.factory.providerForSession(r.defaultSessionID)
	if err != nil {
		return nil, err
	}
	return provider.ListModels(ctx)
}

func (r *routerProvider) Name() string {
	return r.factory.Name()
}

func (r *routerProvider) NotifyCompressionStatusSent(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = r.defaultSessionID
	}
	sessionID = NormalizeSessionID(sessionID)
	provider, err := r.factory.providerForSession(sessionID)
	if err != nil {
		return
	}
	providers.NotifyCompressionStatusSent(provider, sessionID)
}

func (r *routerProvider) resolveSessionID(ctx context.Context, explicit string) string {
	sessionID := strings.TrimSpace(explicit)
	if sessionID == "" {
		sessionID = providers.SessionIDFromContext(ctx)
	}
	if sessionID == "" {
		sessionID = r.defaultSessionID
	}
	return NormalizeSessionID(sessionID)
}

func providerErrorStream(err error) <-chan providers.Msg {
	out := make(chan providers.Msg, 1)
	if err != nil {
		out <- providers.Msg{Type: providers.MsgTypeError, Value: err.Error()}
	}
	close(out)
	return out
}
