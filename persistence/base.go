package persistence

import (
	"context"
	"strings"

	"github.com/fernandopn/benoit/providers"
)

const defaultSessionID = "__default__"

type SessionState struct {
	Provider           providers.ProviderType
	SessionID          string
	PreviousResponseID string
	RemainingTokens    *int64
	UpdatedAtUnix      int64
}

type SessionStore interface {
	GetSession(ctx context.Context, providerType providers.ProviderType, sessionID string) (SessionState, bool, error)
	ListSessions(ctx context.Context, providerType providers.ProviderType) ([]SessionState, error)
	UpdateSession(ctx context.Context, state SessionState) error
	DeleteSession(ctx context.Context, providerType providers.ProviderType, sessionID string) error
	Close() error
}

func NormalizeSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return defaultSessionID
	}
	return sessionID
}
