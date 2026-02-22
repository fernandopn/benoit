package middleware

import (
	"context"
	"sync"
	"testing"

	"github.com/fernandopn/benoit/persistence"
	"github.com/fernandopn/benoit/providers"
	"github.com/fernandopn/benoit/session"
)

type sessionStoreStub struct {
	mu      sync.Mutex
	byKey   map[string]persistence.SessionState
	updates []persistence.SessionState
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
	if s.byKey == nil {
		s.byKey = map[string]persistence.SessionState{}
	}
	state.SessionID = session.NormalizeSessionID(state.SessionID)
	key := s.key(state.Provider, state.SessionID)
	if existing, ok := s.byKey[key]; ok {
		if state.PreviousResponseID == "" {
			state.PreviousResponseID = existing.PreviousResponseID
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
	previousID    string
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
		p.previousID = p.compressionID
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

func (p *cursorProviderStub) PreviousResponseID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.previousID
}

func (p *cursorProviderStub) SetPreviousResponseID(previousResponseID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.previousID = previousResponseID
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
	provider := &cursorProviderStub{chatMsgs: []providers.Msg{
		{Type: providers.MsgTypeChatFinal, Value: "ok", Metadata: map[string]string{"response_id": "resp-1", "tokens_remaining": "77"}},
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
	if state.PreviousResponseID != "resp-1" {
		t.Fatalf("unexpected previous_response_id: %q", state.PreviousResponseID)
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
	if state.PreviousResponseID != "seed-response" {
		t.Fatalf("unexpected previous_response_id: %q", state.PreviousResponseID)
	}
}
