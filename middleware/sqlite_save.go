package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/fernandopn/benoit/providers"
	_ "modernc.org/sqlite"
)

const sqliteSaveSchema = `
CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	direction TEXT NOT NULL,
	msg_type TEXT NOT NULL,
	value TEXT NOT NULL,
	metadata TEXT NOT NULL
);`

type SQLiteSave struct {
	provider providers.Provider
	db       *sql.DB
}

func ConfigureSQLiteSave(ctx context.Context, provider providers.Provider, dbPath string) (providers.Provider, func() error, error) {
	if strings.TrimSpace(dbPath) == "" {
		return provider, nil, nil
	}

	sqliteProvider, err := NewSQLiteSave(ctx, provider, strings.TrimSpace(dbPath))
	if err != nil {
		return nil, nil, err
	}

	return sqliteProvider, sqliteProvider.Close, nil
}

func NewSQLiteSave(ctx context.Context, provider providers.Provider, dbPath string) (*SQLiteSave, error) {
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
	if _, err := db.ExecContext(ctx, sqliteSaveSchema); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, closeErr
		}
		return nil, err
	}
	return &SQLiteSave{provider: provider, db: db}, nil
}

func (s *SQLiteSave) Chat(ctx context.Context, input string) <-chan providers.Msg {
	return s.chat(ctx, input, func() <-chan providers.Msg {
		return s.provider.Chat(ctx, input)
	})
}

func (s *SQLiteSave) ChatInSession(ctx context.Context, input string, sessionID string) <-chan providers.Msg {
	sessionProvider, ok := s.provider.(providers.SessionProvider)
	if !ok {
		return s.Chat(ctx, input)
	}
	return s.chat(ctx, input, func() <-chan providers.Msg {
		return sessionProvider.ChatInSession(ctx, input, sessionID)
	})
}

func (s *SQLiteSave) chat(ctx context.Context, input string, start func() <-chan providers.Msg) <-chan providers.Msg {
	out := make(chan providers.Msg, 4)
	if err := s.storeInput(ctx, input); err != nil {
		out <- storageErrorMsg("store_input", err)
	}
	in := start()

	go func() {
		defer close(out)
		var aggBuf strings.Builder
		var aggType providers.MsgType
		aggActive := false

		flushAgg := func() {
			if !aggActive || aggBuf.Len() == 0 {
				aggActive = false
				return
			}
			agg := providers.Msg{Type: aggType, Value: aggBuf.String()}
			if err := s.storeReceived(ctx, agg); err != nil {
				out <- storageErrorMsg("store_received", err)
			}
			aggBuf.Reset()
			aggActive = false
		}

		for msg := range in {
			out <- msg
			if msg.Type == providers.MsgTypeChat || msg.Type == providers.MsgTypeReasoningSummary {
				if !aggActive || aggType != msg.Type {
					flushAgg()
					aggType = msg.Type
					aggActive = true
				}
				aggBuf.WriteString(msg.Value)
				continue
			}

			flushAgg()
			if err := s.storeReceived(ctx, msg); err != nil {
				out <- storageErrorMsg("store_received", err)
			}
		}
		flushAgg()
	}()

	return out
}

func (s *SQLiteSave) ListModels(ctx context.Context) ([]string, error) {
	return s.provider.ListModels(ctx)
}

func (s *SQLiteSave) Name() string {
	return s.provider.Name()
}

func (s *SQLiteSave) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteSave) storeInput(ctx context.Context, input string) error {
	if s.db == nil {
		return nil
	}
	metadata := "{}"
	_, err := s.db.ExecContext(ctx, `INSERT INTO messages (direction, msg_type, value, metadata) VALUES (?, ?, ?, ?)`,
		"sent", msgTypeString(providers.MsgTypeChat), input, metadata)
	return err
}

func (s *SQLiteSave) storeReceived(ctx context.Context, msg providers.Msg) error {
	if s.db == nil {
		return nil
	}
	metadata := "{}"
	if len(msg.Metadata) > 0 {
		if encoded, err := json.Marshal(msg.Metadata); err == nil {
			metadata = string(encoded)
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO messages (direction, msg_type, value, metadata) VALUES (?, ?, ?, ?)`,
		"received", msgTypeString(msg.Type), msg.Value, metadata)
	return err
}

func msgTypeString(msgType providers.MsgType) string {
	switch msgType {
	case providers.MsgTypeChat:
		return "chat"
	case providers.MsgTypeReasoningSummary:
		return "reasoning_summary"
	case providers.MsgTypeError:
		return "error"
	case providers.MsgTypeToolCall:
		return "tool_call"
	case providers.MsgTypeToolResult:
		return "tool_result"
	case providers.MsgTypeContextUsage:
		return "context_usage"
	default:
		return "unknown"
	}
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
