package persistence

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/fernandopn/benoit/providers"
)

func TestSessionStoreUpdateAndGet(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "session.db")
	db, closeDB, err := ConfigureDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("configure db: %v", err)
	}
	if closeDB != nil {
		defer closeDB()
	}

	store, err := NewSessionStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}

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

func TestConfigureDB(t *testing.T) {
	db, closeFn, err := ConfigureDB(context.Background(), "   ")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if db != nil {
		t.Fatalf("expected nil db, got %#v", db)
	}
	if closeFn != nil {
		t.Fatal("expected nil close function")
	}
}
