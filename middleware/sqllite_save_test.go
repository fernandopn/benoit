package middleware

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/fernandopn/benoit/providers"
)

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
		{Type: providers.MsgTypeChat, Value: "hello"},
		{Type: providers.MsgTypeReasoningSummary, Value: "thinking"},
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

	rows, err := db.Query(`SELECT direction, msg_type, value FROM messages ORDER BY id`)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	defer rows.Close()

	type row struct {
		direction string
		msgType   string
		value     string
	}

	got := []row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.direction, &r.msgType, &r.value); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}

	if len(got) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(got))
	}

	if got[0].direction != "sent" || got[0].msgType != "chat" || got[0].value != "hi" {
		t.Fatalf("unexpected first row: %#v", got[0])
	}
	if got[1].direction != "received" || got[1].msgType != "chat" || got[1].value != "hello" {
		t.Fatalf("unexpected chat row: %#v", got[1])
	}
	if got[2].direction != "received" || got[2].msgType != "reasoning_summary" || got[2].value != "thinking" {
		t.Fatalf("unexpected reasoning row: %#v", got[2])
	}
	if got[3].direction != "received" || got[3].msgType != "tool_result" || got[3].value != "ok" {
		t.Fatalf("unexpected tool result row: %#v", got[3])
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
		provider: &stubProvider{messages: []providers.Msg{{Type: providers.MsgTypeChat, Value: "hello"}}},
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
	base := &sessionStubProvider{stubProvider: stubProvider{messages: []providers.Msg{{Type: providers.MsgTypeChat, Value: "ok"}}}}
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
