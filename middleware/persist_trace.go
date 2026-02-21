package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/fernandopn/benoit/persistence"
	"github.com/fernandopn/benoit/providers"
	_ "modernc.org/sqlite"
)

const persistTraceSchema = `
CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	provider INTEGER NOT NULL,
	session_id TEXT NOT NULL,
	msg_type TEXT NOT NULL,
	value TEXT NOT NULL,
	metadata TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_provider_session_id ON messages(provider, session_id, id);`

const persistTraceMsgTypeInput = "input"

type PersistTrace struct {
	provider     providers.Provider
	providerType providers.ProviderType
	sessionID    string
	db           *sql.DB
}

func ConfigurePersistTrace(ctx context.Context, provider providers.Provider, providerType providers.ProviderType, sessionID string, dbPath string) (providers.Provider, func() error, error) {
	if strings.TrimSpace(dbPath) == "" {
		return provider, nil, nil
	}

	traceProvider, err := NewPersistTrace(ctx, provider, providerType, sessionID, strings.TrimSpace(dbPath))
	if err != nil {
		return nil, nil, err
	}

	return traceProvider, traceProvider.Close, nil
}

func NewPersistTrace(ctx context.Context, provider providers.Provider, providerType providers.ProviderType, sessionID string, dbPath string) (*PersistTrace, error) {
	if provider == nil {
		return nil, errors.New("provider is required")
	}
	if dbPath == "" {
		return nil, errors.New("db path is required")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, persistTraceSchema); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, closeErr
		}
		return nil, err
	}
	return &PersistTrace{
		provider:     provider,
		providerType: providerType,
		sessionID:    persistence.NormalizeSessionID(sessionID),
		db:           db,
	}, nil
}

func (s *PersistTrace) Chat(ctx context.Context, input string) <-chan providers.Msg {
	return s.chat(ctx, input, func() <-chan providers.Msg {
		return s.provider.Chat(ctx, input)
	})
}

func (s *PersistTrace) PerformCompression(ctx context.Context, sessionID string, compressor providers.Compressor) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = s.sessionID
	}
	return s.provider.PerformCompression(ctx, sessionID, compressor)
}

func (s *PersistTrace) chat(ctx context.Context, input string, start func() <-chan providers.Msg) <-chan providers.Msg {
	out := make(chan providers.Msg, 4)
	if err := s.storeInput(ctx, input); err != nil {
		out <- storageErrorMsg("store_input", err)
	}
	in := start()

	go func() {
		defer close(out)

		for msg := range in {
			out <- msg
			if err := s.storeReceived(ctx, msg); err != nil {
				out <- storageErrorMsg("store_received", err)
			}
		}
	}()

	return out
}

func (s *PersistTrace) ListModels(ctx context.Context) ([]string, error) {
	return s.provider.ListModels(ctx)
}

func (s *PersistTrace) Name() string {
	return s.provider.Name()
}

func (s *PersistTrace) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *PersistTrace) storeInput(ctx context.Context, input string) error {
	if s.db == nil {
		return nil
	}
	metadata := "{}"
	_, err := s.db.ExecContext(ctx, `INSERT INTO messages (provider, session_id, msg_type, value, metadata) VALUES (?, ?, ?, ?, ?)`,
		int(s.providerType), s.sessionID, persistTraceMsgTypeInput, input, metadata)
	return err
}

func (s *PersistTrace) storeReceived(ctx context.Context, msg providers.Msg) error {
	if s.db == nil {
		return nil
	}
	metadata := "{}"
	if len(msg.Metadata) > 0 {
		if encoded, err := json.Marshal(msg.Metadata); err == nil {
			metadata = string(encoded)
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO messages (provider, session_id, msg_type, value, metadata) VALUES (?, ?, ?, ?, ?)`,
		int(s.providerType), s.sessionID, msg.Type.StorageValue(), msg.Value, metadata)
	return err
}

func storageErrorMsg(phase string, err error) providers.Msg {
	return providers.Msg{
		Type:  providers.MsgTypeError,
		Value: "storage error while " + phase + ": " + err.Error(),
		Metadata: map[string]string{
			"component": "sqlite",
			"phase":     phase,
		},
	}
}
