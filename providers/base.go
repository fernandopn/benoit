package providers

import "context"

type MsgType int

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

// SessionProvider extends Provider with explicit conversation session routing.
type SessionProvider interface {
	Provider
	// ChatInSession runs chat in the provided logical session.
	ChatInSession(ctx context.Context, input string, sessionID string) <-chan Msg
}
