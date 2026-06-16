package middleware

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/fernandopn/benoit/persistence"
	"github.com/fernandopn/benoit/providers"
	"github.com/fernandopn/benoit/session"
)

type sessionStoreStub struct {
	mu        sync.Mutex
	byKey     map[string]persistence.SessionState
	updates   []persistence.SessionState
	updateErr error
}

func (s *sessionStoreStub) GetSession(_ context.Context, providerType providers.ProviderType, sessionID string) (persistence.SessionState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byKey == nil {
		s.byKey = map[string]persistence.SessionState{}
	}
	state, ok := s.byKey[s.key(providerType, sessionID)]
	return state, ok, nil
}

func (s *sessionStoreStub) ListSessions(_ context.Context, providerType providers.ProviderType) ([]persistence.SessionState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]persistence.SessionState, 0)
	for _, state := range s.byKey {
		if providerType != providers.ProviderTypeUnknown && state.Provider != providerType {
			continue
		}
		out = append(out, state)
	}
	return out, nil
}

func (s *sessionStoreStub) UpdateSession(_ context.Context, state persistence.SessionState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updateErr != nil {
		return s.updateErr
	}
	if s.byKey == nil {
		s.byKey = map[string]persistence.SessionState{}
	}
	state.SessionID = session.NormalizeSessionID(state.SessionID)
	key := s.key(state.Provider, state.SessionID)
	if existing, ok := s.byKey[key]; ok {
		if state.PreviousResponse == "" {
			state.PreviousResponse = existing.PreviousResponse
		}
		if state.RemainingTokens == nil {
			state.RemainingTokens = existing.RemainingTokens
		}
	}
	s.byKey[key] = state
	s.updates = append(s.updates, state)
	return nil
}

func (s *sessionStoreStub) DeleteSession(_ context.Context, providerType providers.ProviderType, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byKey, s.key(providerType, sessionID))
	return nil
}

func (s *sessionStoreStub) Close() error {
	return nil
}

func (s *sessionStoreStub) key(providerType providers.ProviderType, sessionID string) string {
	return providerType.String() + ":" + session.NormalizeSessionID(sessionID)
}

type cursorProviderStub struct {
	mu            sync.Mutex
	previous      string
	chatMsgs      []providers.Msg
	compressionID string
}

func (p *cursorProviderStub) Chat(_ context.Context, _ string) <-chan providers.Msg {
	p.mu.Lock()
	msgs := append([]providers.Msg(nil), p.chatMsgs...)
	p.mu.Unlock()
	return streamMsgs(msgs...)
}

func (p *cursorProviderStub) PerformCompression(_ context.Context, _ string, _ providers.Compressor) (string, error) {
	p.mu.Lock()
	if p.compressionID != "" {
		p.previous = p.compressionID
	}
	p.mu.Unlock()
	return "summary", nil
}

func (p *cursorProviderStub) ListModels(_ context.Context) ([]string, error) {
	return nil, nil
}

func (p *cursorProviderStub) Name() string {
	return "cursor-stub"
}

func (p *cursorProviderStub) ExportPreviousResponse() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.previous, nil
}

func (p *cursorProviderStub) ImportPreviousResponse(serialized string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.previous = serialized
	return nil
}

func streamMsgs(msgs ...providers.Msg) <-chan providers.Msg {
	out := make(chan providers.Msg, len(msgs))
	for _, msg := range msgs {
		out <- msg
	}
	close(out)
	return out
}

func drain(ch <-chan providers.Msg) {
	for range ch {
	}
}

func TestSessionStoreMiddlewareUpdatesFromChatFinal(t *testing.T) {
	remaining := int64(77)
	provider := &cursorProviderStub{previous: "resp-1", chatMsgs: []providers.Msg{
		{Type: providers.MsgTypeChatFinal, Value: "ok", Final: &providers.FinalInfo{RemainingTokens: &remaining}},
	}}
	store := &sessionStoreStub{}
	wrapped := NewSessionStoreMiddleware(provider, store, providers.ProviderTypeOpenAI, "session-1")

	drain(wrapped.Chat(context.Background(), "hello"))

	state, found, err := store.GetSession(context.Background(), providers.ProviderTypeOpenAI, "session-1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !found {
		t.Fatal("expected session state to be stored")
	}
	if state.PreviousResponse != "resp-1" {
		t.Fatalf("unexpected previous_response: %q", state.PreviousResponse)
	}
	if state.RemainingTokens == nil || *state.RemainingTokens != 77 {
		t.Fatalf("unexpected remaining tokens: %#v", state.RemainingTokens)
	}
}

func TestSessionStoreMiddlewareUpdatesAfterCompression(t *testing.T) {
	provider := &cursorProviderStub{compressionID: "seed-response"}
	store := &sessionStoreStub{}
	wrapped := NewSessionStoreMiddleware(provider, store, providers.ProviderTypeOpenAI, "session-9")

	if _, err := wrapped.PerformCompression(context.Background(), "", nil); err != nil {
		t.Fatalf("perform compression: %v", err)
	}

	state, found, err := store.GetSession(context.Background(), providers.ProviderTypeOpenAI, "session-9")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !found {
		t.Fatal("expected session state update after compression")
	}
	if state.PreviousResponse != "seed-response" {
		t.Fatalf("unexpected previous_response: %q", state.PreviousResponse)
	}
}

func TestSessionStoreMiddlewareSurfacesChatPersistenceErrors(t *testing.T) {
	provider := &cursorProviderStub{previous: "resp-1", chatMsgs: []providers.Msg{
		{Type: providers.MsgTypeChatFinal, Value: "ok"},
	}}
	store := &sessionStoreStub{updateErr: errors.New("db unavailable")}
	wrapped := NewSessionStoreMiddleware(provider, store, providers.ProviderTypeOpenAI, "session-1")

	seenStorageError := false
	for msg := range wrapped.Chat(context.Background(), "hello") {
		if msg.Type != providers.MsgTypeError {
			continue
		}
		if msg.Extra["component"] != "persistence" || msg.Extra["phase"] != "update_session" {
			t.Fatalf("unexpected error extra: %#v", msg.Extra)
		}
		if !strings.Contains(msg.Value, "db unavailable") {
			t.Fatalf("unexpected error value: %q", msg.Value)
		}
		seenStorageError = true
	}
	if !seenStorageError {
		t.Fatal("expected storage error message")
	}
}

func TestSessionStoreMiddlewareReturnsCompressionSyncErrors(t *testing.T) {
	provider := &cursorProviderStub{compressionID: "seed-response"}
	store := &sessionStoreStub{updateErr: errors.New("write failed")}
	wrapped := NewSessionStoreMiddleware(provider, store, providers.ProviderTypeOpenAI, "session-9")

	_, err := wrapped.PerformCompression(context.Background(), "", nil)
	if err == nil {
		t.Fatal("expected compression sync error")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
