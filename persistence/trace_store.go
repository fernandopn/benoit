package persistence

import (
	"context"

	"github.com/uptrace/bun"
)

type TraceMessageStore interface {
	Init(context.Context) error
	InsertMessage(context.Context, *TraceMessageModel) error
}

func NewTraceMessageStore(ctx context.Context, db *bun.DB) (*BunTraceMessageStore, error) {
	if db == nil {
		return nil, nil
	}

	store := &BunTraceMessageStore{db: db}
	if err := store.Init(ctx); err != nil {
		return nil, err
	}

	return store, nil
}

type BunTraceMessageStore struct {
	db *bun.DB
}

func (s *BunTraceMessageStore) Init(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	return EnsureTraceMessageSchema(ctx, s.db)
}

func (s *BunTraceMessageStore) InsertMessage(ctx context.Context, record *TraceMessageModel) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.NewInsert().Model(record).Exec(ctx)
	return err
}
