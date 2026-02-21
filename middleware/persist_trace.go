package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/fernandopn/benoit/persistence"
	"github.com/fernandopn/benoit/providers"
	"github.com/uptrace/bun"
)

type PersistTrace struct {
	provider     providers.Provider
	providerType providers.ProviderType
	sessionID    string
	db           *bun.DB
}

func ConfigurePersistTrace(ctx context.Context, provider providers.Provider, providerType providers.ProviderType, sessionID string, db *bun.DB) (providers.Provider, func() error, error) {
	if db == nil {
		return provider, nil, nil
	}

	traceProvider, err := NewPersistTrace(ctx, provider, providerType, sessionID, db)
	if err != nil {
		return nil, nil, err
	}

	return traceProvider, traceProvider.Close, nil
}

func NewPersistTrace(ctx context.Context, provider providers.Provider, providerType providers.ProviderType, sessionID string, db *bun.DB) (*PersistTrace, error) {
	if provider == nil {
		return nil, errors.New("provider is required")
	}
	if db == nil {
		return nil, errors.New("db is required")
	}
	trace, err := newPersistTrace(ctx, provider, providerType, sessionID, db)
	if err != nil {
		return nil, err
	}
	return trace, nil
}

func newPersistTrace(ctx context.Context, provider providers.Provider, providerType providers.ProviderType, sessionID string, db *bun.DB) (*PersistTrace, error) {
	if err := persistence.EnsureTraceMessageSchema(ctx, db); err != nil {
		return nil, err
	}
	return &PersistTrace{
		provider:     provider,
		providerType: providerType,
		sessionID:    persistence.NormalizeSessionID(sessionID),
		db:           db,
	}, nil
}

func (s *PersistTrace) Chat(ctx context.Context, input string) <-chan providers.Msg {
	return s.chat(ctx, input, func() <-chan providers.Msg {
		return s.provider.Chat(ctx, input)
	})
}

func (s *PersistTrace) PerformCompression(ctx context.Context, sessionID string, compressor providers.Compressor) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = s.sessionID
	}
	return s.provider.PerformCompression(ctx, sessionID, compressor)
}

func (s *PersistTrace) chat(ctx context.Context, input string, start func() <-chan providers.Msg) <-chan providers.Msg {
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

func (s *PersistTrace) ListModels(ctx context.Context) ([]string, error) {
	return s.provider.ListModels(ctx)
}

func (s *PersistTrace) Name() string {
	return s.provider.Name()
}

func (s *PersistTrace) Close() error {
	return nil
}

func (s *PersistTrace) storeInput(ctx context.Context, input string) error {
	if s.db == nil {
		return nil
	}
	record := &persistence.TraceMessageModel{
		Provider:  int(s.providerType),
		SessionID: s.sessionID,
		MsgType:   persistence.TraceInputMsgType(),
		Value:     input,
		Metadata:  "{}",
	}
	_, err := s.db.NewInsert().Model(record).Exec(ctx)
	return err
}

func (s *PersistTrace) storeReceived(ctx context.Context, msg providers.Msg) error {
	if s.db == nil {
		return nil
	}
	metadata := "{}"
	if len(msg.Metadata) > 0 {
		if encoded, err := json.Marshal(msg.Metadata); err == nil {
			metadata = string(encoded)
		}
	}
	record := &persistence.TraceMessageModel{
		Provider:  int(s.providerType),
		SessionID: s.sessionID,
		MsgType:   msg.Type.StorageValue(),
		Value:     msg.Value,
		Metadata:  metadata,
	}
	_, err := s.db.NewInsert().Model(record).Exec(ctx)
	return err
}

func storageErrorMsg(phase string, err error) providers.Msg {
	return providers.Msg{
		Type:  providers.MsgTypeError,
		Value: "storage error while " + phase + ": " + err.Error(),
		Metadata: map[string]string{
			"component": "persistence",
			"phase":     phase,
		},
	}
}
