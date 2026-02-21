package persistence

import (
	"context"

	"github.com/fernandopn/benoit/providers"
	"github.com/fernandopn/benoit/sessionid"
)

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
	return sessionid.Normalize(sessionID)
}
