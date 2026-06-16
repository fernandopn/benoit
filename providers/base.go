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
	ProviderTypeOpenRouter
)

func (providerType ProviderType) String() string {
	switch providerType {
	case ProviderTypeOpenAI:
		return "openai"
	case ProviderTypeOpenRouter:
		return "openrouter"
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
type contextUsageTargetKey struct{}
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

// WithContextUsageTarget attaches a destination message pointer where
// compressors/providers can write a context usage message when available.
func WithContextUsageTarget(ctx context.Context, target *Msg) context.Context {
	if ctx == nil || target == nil {
		return ctx
	}
	return context.WithValue(ctx, contextUsageTargetKey{}, target)
}

// SetContextUsage writes a context usage message into the target configured
// on the context, returning true when written.
func SetContextUsage(ctx context.Context, msg Msg) bool {
	if ctx == nil || msg.Type != MsgTypeContextUsage {
		return false
	}
	target, ok := ctx.Value(contextUsageTargetKey{}).(*Msg)
	if !ok || target == nil {
		return false
	}
	*target = msg
	return true
}

// WithSessionID attaches a logical session identifier to context.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if ctx == nil {
		return nil
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

func PerformCompressionWithStatus(ctx context.Context, provider Provider, sessionID string, compressor Compressor, fallbackStatus string) (string, Msg, Msg, error) {
	if ctx == nil {
		return "", Msg{}, Msg{}, errors.New("context is required")
	}
	if provider == nil {
		return "", Msg{}, Msg{}, errors.New("provider is required")
	}
	if compressor == nil {
		return "", Msg{}, Msg{}, errors.New("compressor is required")
	}

	fallbackStatus = strings.TrimSpace(fallbackStatus)
	if fallbackStatus == "" {
		fallbackStatus = DefaultCompressionFinishedMessage
	}

	status := Msg{}
	contextUsage := Msg{}
	compressionCtx := WithCompressionStatusTarget(ctx, &status)
	compressionCtx = WithContextUsageTarget(compressionCtx, &contextUsage)
	summary, err := provider.PerformCompression(compressionCtx, sessionID, compressor)
	if err != nil {
		return "", Msg{}, Msg{}, err
	}

	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "", Msg{}, Msg{}, errors.New("compression returned empty summary")
	}

	if status.Type != MsgTypeCompressionStatus {
		status = Msg{Type: MsgTypeCompressionStatus, Value: fallbackStatus}
	}
	if strings.TrimSpace(status.Value) == "" {
		status.Value = fallbackStatus
	}

	if inferredContextUsage, ok := contextUsageFromCompressionStatus(status); ok {
		contextUsage = inferredContextUsage
	} else if contextUsage.Type != MsgTypeContextUsage {
		contextUsage = Msg{}
	}

	return summary, status, contextUsage, nil
}

func contextUsageFromCompressionStatus(status Msg) (Msg, bool) {
	if status.Metadata == nil {
		return Msg{}, false
	}
	used := strings.TrimSpace(status.Metadata["to_tokens_used"])
	if used == "" {
		used = strings.TrimSpace(status.Metadata["tokens_input_used"])
	}
	if used == "" {
		used = strings.TrimSpace(status.Metadata["tokens_used"])
	}
	available := strings.TrimSpace(status.Metadata["to_tokens_available"])
	if available == "" {
		available = strings.TrimSpace(status.Metadata["tokens_available"])
	}
	if used == "" || available == "" {
		return Msg{}, false
	}

	metadata := map[string]string{
		"tokens_used":       used,
		"tokens_input_used": used,
		"tokens_available":  available,
	}
	if output := strings.TrimSpace(status.Metadata["to_tokens_output_used"]); output != "" {
		metadata["tokens_output_used"] = output
	}
	if total := strings.TrimSpace(status.Metadata["to_tokens_total_used"]); total != "" {
		metadata["tokens_total_used"] = total
	}
	return Msg{Type: MsgTypeContextUsage, Metadata: metadata}, true
}

func NotifyCompressionStatusSent(provider Provider, sessionID string) {
	notifier, ok := provider.(CompressionStatusSentNotifier)
	if !ok {
		return
	}
	notifier.NotifyCompressionStatusSent(strings.TrimSpace(sessionID))
}

// PreviousResponse is a provider-specific session cursor that is serialized to
// JSON for persistence. OpenAI stores a response id; stateless providers such as
// OpenRouter store the full conversation history.
type PreviousResponse interface {
	isPreviousResponse()
}

// SessionCursorProvider exposes the provider's session cursor as JSON so
// persistence middleware can hydrate and synchronize it across restarts.
type SessionCursorProvider interface {
	// ExportPreviousResponse serializes the current cursor ("" when empty).
	ExportPreviousResponse() (string, error)
	// ImportPreviousResponse restores a cursor produced by ExportPreviousResponse.
	ImportPreviousResponse(serialized string) error
}
