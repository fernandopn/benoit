package providers

import (
	"context"
	"strings"
)

type MsgType int

type ProviderType int

const (
	ProviderTypeUnknown ProviderType = iota
	ProviderTypeOpenAI
)

func (providerType ProviderType) String() string {
	switch providerType {
	case ProviderTypeOpenAI:
		return "openai"
	default:
		return "unknown"
	}
}

const (
	MsgTypeChatDelta MsgType = iota
	MsgTypeChatFinal
	MsgTypeReasoningSummaryDelta
	MsgTypeReasoningSummaryFinal
	MsgTypeError
	MsgTypeToolCall
	MsgTypeToolResult
	MsgTypeContextUsage
	MsgTypeCompressionStatus
)

var msgTypeStorageValue = map[MsgType]string{
	MsgTypeChatDelta:             "chat_delta",
	MsgTypeChatFinal:             "chat_final",
	MsgTypeReasoningSummaryDelta: "reasoning_summary_delta",
	MsgTypeReasoningSummaryFinal: "reasoning_summary_final",
	MsgTypeError:                 "error",
	MsgTypeToolCall:              "tool_call",
	MsgTypeToolResult:            "tool_result",
	MsgTypeContextUsage:          "context_usage",
	MsgTypeCompressionStatus:     "compression_status",
}

func (msgType MsgType) StorageValue() string {
	if value, ok := msgTypeStorageValue[msgType]; ok {
		return value
	}
	return "unknown"
}

// Msg represents a message emitted by a provider.
type Msg struct {
	Type     MsgType
	Value    string
	Metadata map[string]string
}

// Compressor produces a compressed summary for a provider session.
type Compressor interface {
	Compress(ctx context.Context, provider Provider, sessionID string) (string, error)
}

type compressionStatusTargetKey struct{}
type sessionIDContextKey struct{}

// WithCompressionStatusTarget attaches a destination message pointer where
// providers can write a compression status message when available.
func WithCompressionStatusTarget(ctx context.Context, target *Msg) context.Context {
	if ctx == nil || target == nil {
		return ctx
	}
	return context.WithValue(ctx, compressionStatusTargetKey{}, target)
}

// SetCompressionStatus writes a compression status message into the target
// configured on the context, returning true when written.
func SetCompressionStatus(ctx context.Context, msg Msg) bool {
	if ctx == nil {
		return false
	}
	target, ok := ctx.Value(compressionStatusTargetKey{}).(*Msg)
	if !ok || target == nil {
		return false
	}
	*target = msg
	return true
}

// WithSessionID attaches a logical session identifier to context.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionIDContextKey{}, sessionID)
}

// SessionIDFromContext returns the logical session identifier from context.
func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(sessionIDContextKey{}).(string)
	return strings.TrimSpace(value)
}

// Provider abstracts the chat interaction for a model backend.
type Provider interface {
	// Chat returns a stream of messages. The stream is done when the channel closes.
	Chat(ctx context.Context, input string) <-chan Msg
	// PerformCompression compresses, resets, and re-seeds session context.
	PerformCompression(ctx context.Context, sessionID string, compressor Compressor) (string, error)
	// ListModels returns the available model IDs for the provider.
	ListModels(ctx context.Context) ([]string, error)
	// Name returns the provider display name.
	Name() string
}

// SessionCursorProvider exposes mutable session response cursor so
// persistence middleware can hydrate and synchronize provider state.
type SessionCursorProvider interface {
	PreviousResponseID() string
	SetPreviousResponseID(previousResponseID string)
}
