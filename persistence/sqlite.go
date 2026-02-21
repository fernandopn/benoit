package persistence

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/fernandopn/benoit/providers"
	_ "modernc.org/sqlite"
)

const sqliteSessionStoreSchema = `
CREATE TABLE IF NOT EXISTS session_state (
	provider INTEGER NOT NULL,
	session_id TEXT NOT NULL,
	previous_response_id TEXT NOT NULL DEFAULT '',
	remaining_tokens INTEGER,
	updated_at INTEGER NOT NULL,
	PRIMARY KEY (provider, session_id)
);
CREATE INDEX IF NOT EXISTS idx_session_state_updated_at ON session_state(updated_at);`

type SQLiteSessionStore struct {
	db *sql.DB
}

func ConfigureSQLiteSessionStore(ctx context.Context, dbPath string) (SessionStore, func() error, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, nil, nil
	}
	store, err := NewSQLiteSessionStore(ctx, dbPath)
	if err != nil {
		return nil, nil, err
	}
	return store, store.Close, nil
}

func NewSQLiteSessionStore(ctx context.Context, dbPath string) (*SQLiteSessionStore, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, errors.New("db path is required")
	}
	db, err := sql.Open("sqlite", strings.TrimSpace(dbPath))
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, sqliteSessionStoreSchema); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, closeErr
		}
		return nil, err
	}
	return &SQLiteSessionStore{db: db}, nil
}

func (s *SQLiteSessionStore) GetSession(ctx context.Context, providerType providers.ProviderType, sessionID string) (SessionState, bool, error) {
	if s == nil || s.db == nil {
		return SessionState{}, false, nil
	}
	normalizedSessionID := NormalizeSessionID(sessionID)
	row := s.db.QueryRowContext(ctx, `
		SELECT provider, session_id, previous_response_id, remaining_tokens, updated_at
		FROM session_state
		WHERE provider = ? AND session_id = ?
	`, int(providerType), normalizedSessionID)
	state := SessionState{}
	var remaining sql.NullInt64
	if err := row.Scan(&state.Provider, &state.SessionID, &state.PreviousResponseID, &remaining, &state.UpdatedAtUnix); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionState{}, false, nil
		}
		return SessionState{}, false, err
	}
	if remaining.Valid {
		value := remaining.Int64
		state.RemainingTokens = &value
	}
	return state, true, nil
}

func (s *SQLiteSessionStore) ListSessions(ctx context.Context, providerType providers.ProviderType) ([]SessionState, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	query := `
		SELECT provider, session_id, previous_response_id, remaining_tokens, updated_at
		FROM session_state
	`
	args := []any{}
	if providerType != providers.ProviderTypeUnknown {
		query += `WHERE provider = ? `
		args = append(args, int(providerType))
	}
	query += `ORDER BY updated_at DESC, provider ASC, session_id ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]SessionState, 0)
	for rows.Next() {
		state := SessionState{}
		var remaining sql.NullInt64
		if err := rows.Scan(&state.Provider, &state.SessionID, &state.PreviousResponseID, &remaining, &state.UpdatedAtUnix); err != nil {
			return nil, err
		}
		if remaining.Valid {
			value := remaining.Int64
			state.RemainingTokens = &value
		}
		out = append(out, state)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SQLiteSessionStore) UpdateSession(ctx context.Context, state SessionState) error {
	if s == nil || s.db == nil {
		return nil
	}
	normalizedSessionID := NormalizeSessionID(state.SessionID)
	updatedAt := state.UpdatedAtUnix
	if updatedAt <= 0 {
		updatedAt = time.Now().Unix()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO session_state (provider, session_id, previous_response_id, remaining_tokens, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(provider, session_id) DO UPDATE SET
			previous_response_id = CASE
				WHEN excluded.previous_response_id != '' THEN excluded.previous_response_id
				ELSE session_state.previous_response_id
			END,
			remaining_tokens = CASE
				WHEN excluded.remaining_tokens IS NOT NULL THEN excluded.remaining_tokens
				ELSE session_state.remaining_tokens
			END,
			updated_at = excluded.updated_at
	`, int(state.Provider), normalizedSessionID, strings.TrimSpace(state.PreviousResponseID), state.RemainingTokens, updatedAt)
	return err
}

func (s *SQLiteSessionStore) DeleteSession(ctx context.Context, providerType providers.ProviderType, sessionID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM session_state WHERE provider = ? AND session_id = ?`, int(providerType), NormalizeSessionID(sessionID))
	return err
}

func (s *SQLiteSessionStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
