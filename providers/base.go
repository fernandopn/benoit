package providers

import (
	"context"
	"errors"
	"strings"
)

const DefaultCompressionFinishedMessage = "Context compression finished."

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

// CompressionStatusSentNotifier allows providers/middleware to clear
// compression-related barriers once the status message was delivered.
type CompressionStatusSentNotifier interface {
	NotifyCompressionStatusSent(sessionID string)
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

func PerformCompressionWithStatus(ctx context.Context, provider Provider, sessionID string, compressor Compressor, fallbackStatus string) (string, Msg, error) {
	if ctx == nil {
		return "", Msg{}, errors.New("context is required")
	}
	if provider == nil {
		return "", Msg{}, errors.New("provider is required")
	}
	if compressor == nil {
		return "", Msg{}, errors.New("compressor is required")
	}

	fallbackStatus = strings.TrimSpace(fallbackStatus)
	if fallbackStatus == "" {
		fallbackStatus = DefaultCompressionFinishedMessage
	}

	status := Msg{}
	summary, err := provider.PerformCompression(WithCompressionStatusTarget(ctx, &status), sessionID, compressor)
	if err != nil {
		return "", Msg{}, err
	}

	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "", Msg{}, errors.New("compression returned empty summary")
	}

	if status.Type != MsgTypeCompressionStatus {
		status = Msg{Type: MsgTypeCompressionStatus, Value: fallbackStatus}
	}
	if strings.TrimSpace(status.Value) == "" {
		status.Value = fallbackStatus
	}
	return summary, status, nil
}

func NotifyCompressionStatusSent(provider Provider, sessionID string) {
	notifier, ok := provider.(CompressionStatusSentNotifier)
	if !ok {
		return
	}
	notifier.NotifyCompressionStatusSent(strings.TrimSpace(sessionID))
}

// SessionCursorProvider exposes mutable session response cursor so
// persistence middleware can hydrate and synchronize provider state.
type SessionCursorProvider interface {
	PreviousResponseID() string
	SetPreviousResponseID(previousResponseID string)
}
