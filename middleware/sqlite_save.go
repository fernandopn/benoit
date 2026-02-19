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
	msg_type TEXT NOT NULL,
	value TEXT NOT NULL,
	metadata TEXT NOT NULL
);`

const middlewareMsgTypeInput = "input"

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
	if err := migrateSQLiteMessagesTable(ctx, db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, closeErr
		}
		return nil, err
	}
	return &SQLiteSave{provider: provider, db: db}, nil
}

func migrateSQLiteMessagesTable(ctx context.Context, db *sql.DB) error {
	hasDirection, err := tableHasColumn(ctx, db, "messages", "direction")
	if err != nil {
		return err
	}
	if !hasDirection {
		return nil
	}

	if _, err := db.ExecContext(ctx, "ALTER TABLE messages RENAME TO messages_old"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, sqliteSaveSchema); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO messages (id, msg_type, value, metadata)
		SELECT
			id,
			CASE
				WHEN direction = 'sent' THEN ?
				WHEN msg_type = 'chat' THEN 'chat_final'
				WHEN msg_type = 'reasoning_summary' THEN 'reasoning_summary_final'
				ELSE msg_type
			END,
			value,
			metadata
		FROM messages_old
		ORDER BY id
	`, middlewareMsgTypeInput); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE messages_old"); err != nil {
		return err
	}

	return nil
}

func tableHasColumn(ctx context.Context, db *sql.DB, tableName string, columnName string) (bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+tableName+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return false, err
		}
		if strings.EqualFold(name, columnName) {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
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

		for msg := range in {
			out <- msg
			if err := s.storeReceived(ctx, msg); err != nil {
				out <- storageErrorMsg("store_received", err)
			}
		}
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO messages (msg_type, value, metadata) VALUES (?, ?, ?)`,
		middlewareMsgTypeInput, input, metadata)
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO messages (msg_type, value, metadata) VALUES (?, ?, ?)`,
		msgTypeString(msg.Type), msg.Value, metadata)
	return err
}

func msgTypeString(msgType providers.MsgType) string {
	switch msgType {
	case providers.MsgTypeChatDelta:
		return "chat_delta"
	case providers.MsgTypeChatFinal:
		return "chat_final"
	case providers.MsgTypeReasoningSummaryDelta:
		return "reasoning_summary_delta"
	case providers.MsgTypeReasoningSummaryFinal:
		return "reasoning_summary_final"
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
