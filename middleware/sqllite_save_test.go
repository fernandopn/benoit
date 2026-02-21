package middleware

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fernandopn/benoit/providers"
)

const legacySQLiteSaveSchema = `
CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	direction TEXT NOT NULL,
	msg_type TEXT NOT NULL,
	value TEXT NOT NULL,
	metadata TEXT NOT NULL
);`

type stubProvider struct {
	messages []providers.Msg
}

func (s *stubProvider) Chat(_ context.Context, _ string) <-chan providers.Msg {
	out := make(chan providers.Msg, len(s.messages))
	go func() {
		defer close(out)
		for _, msg := range s.messages {
			out <- msg
		}
	}()
	return out
}

func (s *stubProvider) PerformCompression(ctx context.Context, sessionID string, compressor providers.Compressor) (string, error) {
	if compressor == nil {
		return "", errors.New("compressor is required")
	}
	return compressor.Compress(ctx, s, sessionID)
}

func (s *stubProvider) ListModels(_ context.Context) ([]string, error) {
	return nil, nil
}

func (s *stubProvider) Name() string {
	return "stub-provider"
}

type sessionStubProvider struct {
	stubProvider
	sessions []string
}

func (s *sessionStubProvider) ChatInSession(ctx context.Context, input string, sessionID string) <-chan providers.Msg {
	_ = ctx
	_ = input
	s.sessions = append(s.sessions, sessionID)
	return s.Chat(context.Background(), input)
}

func TestSQLiteSavePersistsMessages(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chat.db")
	provider := &stubProvider{messages: []providers.Msg{
		{Type: providers.MsgTypeChatDelta, Value: "hel"},
		{Type: providers.MsgTypeChatDelta, Value: "lo"},
		{Type: providers.MsgTypeChatFinal, Value: "hello"},
		{Type: providers.MsgTypeReasoningSummaryDelta, Value: "think"},
		{Type: providers.MsgTypeReasoningSummaryFinal, Value: "thinking"},
		{Type: providers.MsgTypeToolResult, Value: "ok"},
	}}
	sqlite, err := NewSQLiteSave(context.Background(), provider, dbPath)
	if err != nil {
		t.Fatalf("new sqlite save: %v", err)
	}
	defer sqlite.Close()

	out := sqlite.Chat(context.Background(), "hi")
	for range out {
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite for assertion: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT msg_type, value FROM messages ORDER BY id`)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	defer rows.Close()

	type row struct {
		msgType string
		value   string
	}

	got := []row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.msgType, &r.value); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}

	if len(got) != 7 {
		t.Fatalf("expected 7 rows, got %d", len(got))
	}

	if got[0].msgType != "input" || got[0].value != "hi" {
		t.Fatalf("unexpected first row: %#v", got[0])
	}
	if got[1].msgType != "chat_delta" || got[1].value != "hel" {
		t.Fatalf("unexpected first chat delta row: %#v", got[1])
	}
	if got[2].msgType != "chat_delta" || got[2].value != "lo" {
		t.Fatalf("unexpected second chat delta row: %#v", got[2])
	}
	if got[3].msgType != "chat_final" || got[3].value != "hello" {
		t.Fatalf("unexpected chat final row: %#v", got[3])
	}
	if got[4].msgType != "reasoning_summary_delta" || got[4].value != "think" {
		t.Fatalf("unexpected reasoning delta row: %#v", got[4])
	}
	if got[5].msgType != "reasoning_summary_final" || got[5].value != "thinking" {
		t.Fatalf("unexpected reasoning final row: %#v", got[5])
	}
	if got[6].msgType != "tool_result" || got[6].value != "ok" {
		t.Fatalf("unexpected tool result row: %#v", got[6])
	}
}

func TestSQLiteSaveMigratesLegacySchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-chat.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(legacySQLiteSaveSchema); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO messages (direction, msg_type, value, metadata) VALUES
		('sent', 'chat', 'legacy input', '{}'),
		('received', 'chat', 'legacy output', '{}'),
		('received', 'reasoning_summary', 'legacy reasoning', '{}')`); err != nil {
		t.Fatalf("seed legacy rows: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy sqlite: %v", err)
	}

	sqlite, err := NewSQLiteSave(context.Background(), &stubProvider{}, dbPath)
	if err != nil {
		t.Fatalf("new sqlite save: %v", err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatalf("close sqlite save: %v", err)
	}

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite for assertion: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`PRAGMA table_info(messages)`)
	if err != nil {
		t.Fatalf("query table info: %v", err)
	}
	defer rows.Close()

	hasDirection := false
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
			t.Fatalf("scan table info: %v", err)
		}
		if strings.EqualFold(name, "direction") {
			hasDirection = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table info rows err: %v", err)
	}
	if hasDirection {
		t.Fatal("expected legacy direction column to be removed")
	}

	msgRows, err := db.Query(`SELECT msg_type, value FROM messages ORDER BY id`)
	if err != nil {
		t.Fatalf("query migrated rows: %v", err)
	}
	defer msgRows.Close()

	var got []string
	for msgRows.Next() {
		var msgType, value string
		if err := msgRows.Scan(&msgType, &value); err != nil {
			t.Fatalf("scan migrated row: %v", err)
		}
		got = append(got, msgType+":"+value)
	}
	if err := msgRows.Err(); err != nil {
		t.Fatalf("migrated rows err: %v", err)
	}

	want := []string{
		"input:legacy input",
		"chat_final:legacy output",
		"reasoning_summary_final:legacy reasoning",
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected migrated row count %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d mismatch: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestSQLiteSavePropagatesStorageErrors(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	sqlite := &SQLiteSave{
		provider: &stubProvider{messages: []providers.Msg{{Type: providers.MsgTypeChatFinal, Value: "hello"}}},
		db:       db,
	}

	out := sqlite.Chat(context.Background(), "hi")
	seenError := false
	for msg := range out {
		if msg.Type == providers.MsgTypeError {
			seenError = true
			if msg.Metadata["component"] != "sqlite" {
				t.Fatalf("unexpected metadata: %#v", msg.Metadata)
			}
		}
	}
	if !seenError {
		t.Fatal("expected storage error message")
	}
}

func TestConfigureSQLiteSave(t *testing.T) {
	t.Run("no path disables middleware", func(t *testing.T) {
		base := &stubProvider{}
		configured, closeFn, err := ConfigureSQLiteSave(context.Background(), base, "   ")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if configured != base {
			t.Fatalf("expected original provider to be returned, got %#v", configured)
		}
		if closeFn != nil {
			t.Fatal("expected nil close function")
		}
	})

	t.Run("invalid path returns error", func(t *testing.T) {
		base := &stubProvider{}
		_, _, err := ConfigureSQLiteSave(context.Background(), base, "/tmp/\x00bad")
		if err == nil {
			t.Fatal("expected db path error")
		}
	})

	t.Run("valid path wraps provider", func(t *testing.T) {
		base := &stubProvider{}
		dbPath := filepath.Join(t.TempDir(), "chat.sqlite")
		configured, closeFn, err := ConfigureSQLiteSave(context.Background(), base, dbPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if configured == base {
			t.Fatal("expected middleware wrapper, got base provider")
		}
		if closeFn == nil {
			t.Fatal("expected close function")
		}
		if err := closeFn(); err != nil {
			t.Fatalf("close middleware error: %v", err)
		}
	})
}

func TestSQLiteSaveForwardsSessionChat(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chat.db")
	base := &sessionStubProvider{stubProvider: stubProvider{messages: []providers.Msg{{Type: providers.MsgTypeChatFinal, Value: "ok"}}}}
	sqlite, err := NewSQLiteSave(context.Background(), base, dbPath)
	if err != nil {
		t.Fatalf("new sqlite save: %v", err)
	}
	defer sqlite.Close()

	var provider providers.Provider = sqlite
	sessionProvider, ok := provider.(providers.SessionProvider)
	if !ok {
		t.Fatal("expected sqlite middleware to implement SessionProvider")
	}
	out := sessionProvider.ChatInSession(context.Background(), "hello", "telegram:99")
	for range out {
	}

	if len(base.sessions) != 1 || base.sessions[0] != "telegram:99" {
		t.Fatalf("unexpected sessions forwarded: %v", base.sessions)
	}
}
