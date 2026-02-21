package middleware

import (
	"context"
	"strconv"
	"strings"

	"github.com/fernandopn/benoit/persistence"
	"github.com/fernandopn/benoit/providers"
)

type SessionStoreMiddleware struct {
	provider     providers.Provider
	store        persistence.SessionStore
	providerType providers.ProviderType
	sessionID    string
}

var _ providers.Provider = (*SessionStoreMiddleware)(nil)

func NewSessionStoreMiddleware(provider providers.Provider, store persistence.SessionStore, providerType providers.ProviderType, sessionID string) providers.Provider {
	if provider == nil || store == nil {
		return provider
	}
	return &SessionStoreMiddleware{
		provider:     provider,
		store:        store,
		providerType: providerType,
		sessionID:    persistence.NormalizeSessionID(sessionID),
	}
}

func (m *SessionStoreMiddleware) Chat(ctx context.Context, input string) <-chan providers.Msg {
	return m.wrapStream(ctx, m.provider.Chat(ctx, input))
}

func (m *SessionStoreMiddleware) PerformCompression(ctx context.Context, sessionID string, compressor providers.Compressor) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = m.sessionID
	}
	summary, err := m.provider.PerformCompression(ctx, sessionID, compressor)
	if err != nil {
		return "", err
	}
	m.syncSessionFromProvider(ctx)
	return summary, nil
}

func (m *SessionStoreMiddleware) NotifyCompressionStatusSent(sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = m.sessionID
	}
	providers.NotifyCompressionStatusSent(m.provider, sessionID)
	m.syncSessionFromProvider(context.Background())
}

func (m *SessionStoreMiddleware) ListModels(ctx context.Context) ([]string, error) {
	return m.provider.ListModels(ctx)
}

func (m *SessionStoreMiddleware) Name() string {
	return m.provider.Name()
}

func (m *SessionStoreMiddleware) wrapStream(ctx context.Context, in <-chan providers.Msg) <-chan providers.Msg {
	if in == nil {
		return nil
	}
	out := make(chan providers.Msg)
	go func() {
		defer close(out)
		for msg := range in {
			out <- msg
			m.captureMessage(ctx, msg)
		}
	}()
	return out
}

func (m *SessionStoreMiddleware) captureMessage(ctx context.Context, msg providers.Msg) {
	if m.store == nil {
		return
	}
	responseID := strings.TrimSpace(msg.Metadata["response_id"])
	remaining := remainingTokensFromMsg(msg)
	shouldUpdate := false
	switch msg.Type {
	case providers.MsgTypeChatFinal:
		shouldUpdate = responseID != "" || remaining != nil
	case providers.MsgTypeContextUsage:
		shouldUpdate = remaining != nil
	case providers.MsgTypeCompressionStatus:
		shouldUpdate = responseID != "" || remaining != nil
	default:
		return
	}
	if !shouldUpdate {
		return
	}
	_ = m.store.UpdateSession(ctx, persistence.SessionState{
		Provider:           m.providerType,
		SessionID:          m.sessionID,
		PreviousResponseID: responseID,
		RemainingTokens:    remaining,
	})
}

func (m *SessionStoreMiddleware) syncSessionFromProvider(ctx context.Context) {
	if m.store == nil {
		return
	}
	cursorProvider, ok := m.provider.(providers.SessionCursorProvider)
	if !ok {
		return
	}
	previousID := strings.TrimSpace(cursorProvider.PreviousResponseID())
	if previousID == "" {
		return
	}
	_ = m.store.UpdateSession(ctx, persistence.SessionState{
		Provider:           m.providerType,
		SessionID:          m.sessionID,
		PreviousResponseID: previousID,
	})
}

func remainingTokensFromMsg(msg providers.Msg) *int64 {
	if msg.Metadata == nil {
		return nil
	}
	if remainingRaw := strings.TrimSpace(msg.Metadata["tokens_remaining"]); remainingRaw != "" {
		if remaining, err := strconv.ParseInt(remainingRaw, 10, 64); err == nil {
			return &remaining
		}
	}
	usedRaw := strings.TrimSpace(msg.Metadata["tokens_input_used"])
	if usedRaw == "" {
		usedRaw = strings.TrimSpace(msg.Metadata["tokens_used"])
	}
	availableRaw := strings.TrimSpace(msg.Metadata["tokens_available"])
	if msg.Type == providers.MsgTypeCompressionStatus {
		usedRaw = strings.TrimSpace(msg.Metadata["to_tokens_used"])
		availableRaw = strings.TrimSpace(msg.Metadata["to_tokens_available"])
	}
	if usedRaw == "" || availableRaw == "" {
		return nil
	}
	used, usedErr := strconv.ParseInt(usedRaw, 10, 64)
	available, availableErr := strconv.ParseInt(availableRaw, 10, 64)
	if usedErr != nil || availableErr != nil || available <= 0 {
		return nil
	}
	remaining := available - used
	if remaining < 0 {
		remaining = 0
	}
	return &remaining
}
