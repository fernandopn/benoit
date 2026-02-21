package persistence

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/fernandopn/benoit/providers"
)

func TestSQLiteSessionStoreUpdateAndGet(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "session.db")
	store, err := NewSQLiteSessionStore(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("new sqlite session store: %v", err)
	}
	defer store.Close()

	remaining := int64(321)
	if err := store.UpdateSession(context.Background(), SessionState{
		Provider:           providers.ProviderTypeOpenAI,
		SessionID:          "session-1",
		PreviousResponseID: "resp-1",
		RemainingTokens:    &remaining,
	}); err != nil {
		t.Fatalf("update session: %v", err)
	}

	state, found, err := store.GetSession(context.Background(), providers.ProviderTypeOpenAI, "session-1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !found {
		t.Fatal("expected session state to exist")
	}
	if state.PreviousResponseID != "resp-1" {
		t.Fatalf("unexpected previous_response_id: %q", state.PreviousResponseID)
	}
	if state.RemainingTokens == nil || *state.RemainingTokens != 321 {
		t.Fatalf("unexpected remaining tokens: %#v", state.RemainingTokens)
	}
}

func TestConfigureSQLiteSessionStore(t *testing.T) {
	store, closeFn, err := ConfigureSQLiteSessionStore(context.Background(), "   ")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if store != nil {
		t.Fatalf("expected nil store, got %#v", store)
	}
	if closeFn != nil {
		t.Fatal("expected nil close function")
	}
}
