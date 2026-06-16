package middleware

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/fernandopn/benoit/persistence"
	"github.com/fernandopn/benoit/providers"
	"github.com/fernandopn/benoit/session"
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
		sessionID:    session.NormalizeSessionID(sessionID),
	}
}

func (m *SessionStoreMiddleware) Chat(ctx context.Context, input string) <-chan providers.Msg {
	if ctx == nil {
		return singleErrorMsgStream("context is required")
	}
	return m.wrapStream(ctx, m.provider.Chat(ctx, input))
}

func (m *SessionStoreMiddleware) PerformCompression(ctx context.Context, sessionID string, compressor providers.Compressor) (string, error) {
	if ctx == nil {
		return "", errors.New("context is required")
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = m.sessionID
	}
	summary, err := m.provider.PerformCompression(ctx, sessionID, compressor)
	if err != nil {
		return "", err
	}
	if err := m.syncSessionFromProvider(ctx); err != nil {
		return "", err
	}
	return summary, nil
}

func (m *SessionStoreMiddleware) NotifyCompressionStatusSent(sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = m.sessionID
	}
	providers.NotifyCompressionStatusSent(m.provider, sessionID)
}

func (m *SessionStoreMiddleware) ListModels(ctx context.Context) ([]string, error) {
	return m.provider.ListModels(ctx)
}

func (m *SessionStoreMiddleware) Name() string {
	return m.provider.Name()
}

func (m *SessionStoreMiddleware) wrapStream(ctx context.Context, in <-chan providers.Msg) <-chan providers.Msg {
	if in == nil {
		return singleErrorMsgStream("provider stream is not configured")
	}
	out := make(chan providers.Msg)
	go func() {
		defer close(out)
		for msg := range in {
			out <- msg
			if err := m.captureMessage(ctx, msg); err != nil {
				out <- storageErrorMsg("update_session", err)
			}
		}
	}()
	return out
}

func (m *SessionStoreMiddleware) captureMessage(ctx context.Context, msg providers.Msg) error {
	if m.store == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("context is required")
	}
	remaining := remainingTokensFromMsg(msg)
	previousResponse := ""
	switch msg.Type {
	case providers.MsgTypeChatFinal, providers.MsgTypeCompressionStatus:
		previousResponse = m.exportPreviousResponse()
	case providers.MsgTypeContextUsage:
		// Context usage only carries token counts; the cursor is persisted on
		// chat/compression messages above.
	default:
		return nil
	}
	if previousResponse == "" && remaining == nil {
		return nil
	}
	return m.store.UpdateSession(ctx, persistence.SessionState{
		Provider:         m.providerType,
		SessionID:        m.sessionID,
		PreviousResponse: previousResponse,
		RemainingTokens:  remaining,
	})
}

func (m *SessionStoreMiddleware) syncSessionFromProvider(ctx context.Context) error {
	if m.store == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("context is required")
	}
	previousResponse := m.exportPreviousResponse()
	if previousResponse == "" {
		return nil
	}
	return m.store.UpdateSession(ctx, persistence.SessionState{
		Provider:         m.providerType,
		SessionID:        m.sessionID,
		PreviousResponse: previousResponse,
	})
}

func (m *SessionStoreMiddleware) exportPreviousResponse() string {
	cursorProvider, ok := m.provider.(providers.SessionCursorProvider)
	if !ok {
		return ""
	}
	serialized, err := cursorProvider.ExportPreviousResponse()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(serialized)
}

func singleErrorMsgStream(errText string) <-chan providers.Msg {
	out := make(chan providers.Msg, 1)
	out <- providers.Msg{Type: providers.MsgTypeError, Value: strings.TrimSpace(errText)}
	close(out)
	return out
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
