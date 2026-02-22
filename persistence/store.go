package persistence

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/fernandopn/benoit/providers"
	"github.com/fernandopn/benoit/session"
	"github.com/uptrace/bun"
)

type BunSessionStore struct {
	db *bun.DB
}

func NewSessionStore(ctx context.Context, db *bun.DB) (*BunSessionStore, error) {
	if db == nil {
		return nil, nil
	}
	if err := ensureSessionStoreSchema(ctx, db); err != nil {
		return nil, err
	}
	return &BunSessionStore{db: db}, nil
}

func ensureSessionStoreSchema(ctx context.Context, db *bun.DB) error {
	if db == nil {
		return nil
	}
	if _, err := db.NewCreateTable().Model((*SessionStateModel)(nil)).IfNotExists().Exec(ctx); err != nil {
		return err
	}
	if _, err := db.NewCreateIndex().Model((*SessionStateModel)(nil)).Index("idx_session_state_updated_at").Column("updated_at").IfNotExists().Exec(ctx); err != nil {
		return err
	}
	return nil
}

func (s *BunSessionStore) DB() *bun.DB {
	if s == nil {
		return nil
	}
	return s.db
}

func (s *BunSessionStore) GetSession(ctx context.Context, providerType providers.ProviderType, sessionID string) (SessionState, bool, error) {
	if s == nil || s.db == nil {
		return SessionState{}, false, nil
	}
	model := &SessionStateModel{
		Provider:  int(providerType),
		SessionID: session.NormalizeSessionID(sessionID),
	}
	err := s.db.NewSelect().Model(model).WherePK().Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionState{}, false, nil
		}
		return SessionState{}, false, err
	}
	return sessionStateFromModel(model), true, nil
}

func (s *BunSessionStore) ListSessions(ctx context.Context, providerType providers.ProviderType) ([]SessionState, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	records := make([]SessionStateModel, 0)
	query := s.db.NewSelect().Model(&records)
	if providerType != providers.ProviderTypeUnknown {
		query = query.Where("provider = ?", int(providerType))
	}
	err := query.Order("updated_at DESC").Order("provider ASC").Order("session_id ASC").Scan(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SessionState, 0, len(records))
	for i := range records {
		out = append(out, sessionStateFromModel(&records[i]))
	}
	return out, nil
}

func (s *BunSessionStore) UpdateSession(ctx context.Context, state SessionState) error {
	if s == nil || s.db == nil {
		return nil
	}
	normalizedSessionID := session.NormalizeSessionID(state.SessionID)
	updatedAt := state.UpdatedAtUnix
	if updatedAt <= 0 {
		updatedAt = time.Now().Unix()
	}
	model := &SessionStateModel{
		Provider:         int(state.Provider),
		SessionID:        normalizedSessionID,
		PreviousResponse: strings.TrimSpace(state.PreviousResponseID),
		RemainingTokens:  state.RemainingTokens,
		UpdatedAtUnix:    updatedAt,
	}

	_, err := s.db.NewInsert().Model(model).
		On("CONFLICT (provider, session_id) DO UPDATE").
		Set("previous_response_id = CASE WHEN EXCLUDED.previous_response_id = '' THEN previous_response_id ELSE EXCLUDED.previous_response_id END").
		Set("remaining_tokens = COALESCE(EXCLUDED.remaining_tokens, remaining_tokens)").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

func (s *BunSessionStore) DeleteSession(ctx context.Context, providerType providers.ProviderType, sessionID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	model := &SessionStateModel{Provider: int(providerType), SessionID: session.NormalizeSessionID(sessionID)}
	_, err := s.db.NewDelete().Model(model).WherePK().Exec(ctx)
	return err
}

func (s *BunSessionStore) Close() error {
	return nil
}

func sessionStateFromModel(model *SessionStateModel) SessionState {
	if model == nil {
		return SessionState{}
	}
	return SessionState{
		Provider:           providers.ProviderType(model.Provider),
		SessionID:          model.SessionID,
		PreviousResponseID: model.PreviousResponse,
		RemainingTokens:    model.RemainingTokens,
		UpdatedAtUnix:      model.UpdatedAtUnix,
	}
}
