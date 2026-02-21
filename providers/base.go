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
)

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
