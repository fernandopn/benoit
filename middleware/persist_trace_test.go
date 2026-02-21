package middleware

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/fernandopn/benoit/persistence"
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
	db, closeDB, err := persistence.ConfigureDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("configure db: %v", err)
	}
	if closeDB != nil {
		defer closeDB()
	}

	provider := &persistTraceStubProvider{messages: []providers.Msg{
		{Type: providers.MsgTypeChatDelta, Value: "hel"},
		{Type: providers.MsgTypeChatDelta, Value: "lo"},
		{Type: providers.MsgTypeChatFinal, Value: "hello"},
		{Type: providers.MsgTypeReasoningSummaryDelta, Value: "think"},
		{Type: providers.MsgTypeReasoningSummaryFinal, Value: "thinking"},
		{Type: providers.MsgTypeToolResult, Value: "ok"},
	}}
	trace, err := NewPersistTrace(context.Background(), provider, providers.ProviderTypeOpenAI, "session-77", db)
	if err != nil {
		t.Fatalf("new persist trace: %v", err)
	}
	defer trace.Close()

	out := trace.Chat(context.Background(), "hi")
	for range out {
	}

	got := make([]persistence.TraceMessageModel, 0)
	if err := db.NewSelect().Model(&got).Order("id ASC").Scan(context.Background()); err != nil {
		t.Fatalf("query rows: %v", err)
	}

	if len(got) != 7 {
		t.Fatalf("expected 7 rows, got %d", len(got))
	}

	for i := range got {
		if got[i].Provider != int(providers.ProviderTypeOpenAI) {
			t.Fatalf("unexpected provider at row %d: %d", i, got[i].Provider)
		}
		if got[i].SessionID != "session-77" {
			t.Fatalf("unexpected session at row %d: %q", i, got[i].SessionID)
		}
	}

	if got[0].MsgType != "input" || got[0].Value != "hi" {
		t.Fatalf("unexpected first row: %#v", got[0])
	}
	if got[6].MsgType != "tool_result" || got[6].Value != "ok" {
		t.Fatalf("unexpected final row: %#v", got[6])
	}
}

func TestPersistTracePropagatesStorageErrors(t *testing.T) {
	db, closeDB, err := persistence.ConfigureDB(context.Background(), filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("configure db: %v", err)
	}
	if closeDB == nil {
		t.Fatal("expected close function")
	}
	if err := closeDB(); err != nil {
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
			if msg.Metadata["component"] != "persistence" {
				t.Fatalf("unexpected metadata: %#v", msg.Metadata)
			}
		}
	}
	if !seenError {
		t.Fatal("expected storage error message")
	}
}

func TestNewPersistTrace(t *testing.T) {
	t.Run("requires db", func(t *testing.T) {
		_, err := NewPersistTrace(context.Background(), &persistTraceStubProvider{}, providers.ProviderTypeOpenAI, "session-1", nil)
		if err == nil {
			t.Fatal("expected db error")
		}
	})

	t.Run("returns middleware", func(t *testing.T) {
		base := &persistTraceStubProvider{}
		db, closeDB, err := persistence.ConfigureDB(context.Background(), filepath.Join(t.TempDir(), "chat.db"))
		if err != nil {
			t.Fatalf("configure db: %v", err)
		}
		if closeDB != nil {
			defer closeDB()
		}
		configured, err := NewPersistTrace(context.Background(), base, providers.ProviderTypeOpenAI, "session-1", db)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if configured == nil {
			t.Fatal("expected middleware instance")
		}
		if configured.provider != base {
			t.Fatal("expected wrapped provider")
		}
	})
}
