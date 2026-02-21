package persistence

import (
	"context"

	"github.com/uptrace/bun"
)

const traceInputMsgType = "input"

type SessionStateModel struct {
	bun.BaseModel    `bun:"table:session_state"`
	Provider         int    `bun:"provider,pk,notnull"`
	SessionID        string `bun:"session_id,pk,notnull"`
	PreviousResponse string `bun:"previous_response_id,notnull,default:''"`
	RemainingTokens  *int64 `bun:"remaining_tokens"`
	UpdatedAtUnix    int64  `bun:"updated_at,notnull"`
}

type TraceMessageModel struct {
	bun.BaseModel `bun:"table:messages"`
	ID            int64  `bun:"id,pk,autoincrement"`
	Provider      int    `bun:"provider,notnull"`
	SessionID     string `bun:"session_id,notnull"`
	MsgType       string `bun:"msg_type,notnull"`
	Value         string `bun:"value,notnull"`
	Metadata      string `bun:"metadata,notnull"`
}

func TraceInputMsgType() string {
	return traceInputMsgType
}

func EnsureTraceMessageSchema(ctx context.Context, db *bun.DB) error {
	if db == nil {
		return nil
	}
	if _, err := db.NewCreateTable().Model((*TraceMessageModel)(nil)).IfNotExists().Exec(ctx); err != nil {
		return err
	}
	if _, err := db.NewCreateIndex().Model((*TraceMessageModel)(nil)).Index("idx_messages_provider_session_id").Column("provider", "session_id", "id").IfNotExists().Exec(ctx); err != nil {
		return err
	}
	return nil
}
