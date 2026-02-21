package middleware

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/fernandopn/benoit/providers"
)

type persistTraceStubProvider struct {
	messages []providers.Msg
}

func (s *persistTraceStubProvider) Chat(_ context.Context, _ string) <-chan providers.Msg {
	out := make(chan providers.Msg, len(s.messages))
	go func() {
		defer close(out)
		for _, msg := range s.messages {
			out <- msg
		}
	}()
	return out
}

func (s *persistTraceStubProvider) PerformCompression(ctx context.Context, sessionID string, compressor providers.Compressor) (string, error) {
	if compressor == nil {
		return "", errors.New("compressor is required")
	}
	return compressor.Compress(ctx, s, sessionID)
}

func (s *persistTraceStubProvider) ListModels(_ context.Context) ([]string, error) {
	return nil, nil
}

func (s *persistTraceStubProvider) Name() string {
	return "stub-provider"
}

func TestPersistTracePersistsMessages(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chat.db")
	provider := &persistTraceStubProvider{messages: []providers.Msg{
		{Type: providers.MsgTypeChatDelta, Value: "hel"},
		{Type: providers.MsgTypeChatDelta, Value: "lo"},
		{Type: providers.MsgTypeChatFinal, Value: "hello"},
		{Type: providers.MsgTypeReasoningSummaryDelta, Value: "think"},
		{Type: providers.MsgTypeReasoningSummaryFinal, Value: "thinking"},
		{Type: providers.MsgTypeToolResult, Value: "ok"},
	}}
	trace, err := NewPersistTrace(context.Background(), provider, providers.ProviderTypeOpenAI, "session-77", dbPath)
	if err != nil {
		t.Fatalf("new persist trace: %v", err)
	}
	defer trace.Close()

	out := trace.Chat(context.Background(), "hi")
	for range out {
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite for assertion: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT provider, session_id, msg_type, value FROM messages ORDER BY id`)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	defer rows.Close()

	type row struct {
		provider  int
		sessionID string
		msgType   string
		value     string
	}

	got := []row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.provider, &r.sessionID, &r.msgType, &r.value); err != nil {
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

	for i := range got {
		if got[i].provider != int(providers.ProviderTypeOpenAI) {
			t.Fatalf("unexpected provider at row %d: %d", i, got[i].provider)
		}
		if got[i].sessionID != "session-77" {
			t.Fatalf("unexpected session at row %d: %q", i, got[i].sessionID)
		}
	}

	if got[0].msgType != "input" || got[0].value != "hi" {
		t.Fatalf("unexpected first row: %#v", got[0])
	}
	if got[6].msgType != "tool_result" || got[6].value != "ok" {
		t.Fatalf("unexpected final row: %#v", got[6])
	}
}

func TestPersistTracePropagatesStorageErrors(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	trace := &PersistTrace{
		provider:     &persistTraceStubProvider{messages: []providers.Msg{{Type: providers.MsgTypeChatFinal, Value: "hello"}}},
		providerType: providers.ProviderTypeOpenAI,
		sessionID:    "session-1",
		db:           db,
	}

	out := trace.Chat(context.Background(), "hi")
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

func TestConfigurePersistTrace(t *testing.T) {
	t.Run("no path disables middleware", func(t *testing.T) {
		base := &persistTraceStubProvider{}
		configured, closeFn, err := ConfigurePersistTrace(context.Background(), base, providers.ProviderTypeOpenAI, "session-1", "   ")
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
		base := &persistTraceStubProvider{}
		_, _, err := ConfigurePersistTrace(context.Background(), base, providers.ProviderTypeOpenAI, "session-1", "/tmp/\x00bad")
		if err == nil {
			t.Fatal("expected db path error")
		}
	})

	t.Run("valid path wraps provider", func(t *testing.T) {
		base := &persistTraceStubProvider{}
		dbPath := filepath.Join(t.TempDir(), "chat.sqlite")
		configured, closeFn, err := ConfigurePersistTrace(context.Background(), base, providers.ProviderTypeOpenAI, "session-1", dbPath)
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
