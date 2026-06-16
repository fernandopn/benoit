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
	Type       MsgType
	Value      string
	Usage      *ContextUsage     // set when Type == MsgTypeContextUsage
	ToolCall   *ToolCallInfo     // set when Type == MsgTypeToolCall or MsgTypeToolResult
	Compaction *CompactionStatus // set when Type == MsgTypeCompressionStatus
	Final      *FinalInfo        // set when Type == MsgTypeChatFinal
	Extra      map[string]string // genuinely free-form annotations
}

// ContextUsage carries token accounting for a context usage message.
type ContextUsage struct {
	InputTokensUsed  int64   `json:"input_tokens_used"`
	OutputTokensUsed int64   `json:"output_tokens_used"`
	TotalTokensUsed  int64   `json:"total_tokens_used"`
	ContextWindow    int64   `json:"context_window"`
	TokensAvailable  int64   `json:"tokens_available"`
	PercentUsed      float64 `json:"percent_used"`
}

// ToolCallInfo identifies the tool associated with a tool call or result.
type ToolCallInfo struct {
	Name   string `json:"name"`
	CallID string `json:"call_id"`
}

// CompactionStatus describes a context compaction transition.
type CompactionStatus struct {
	FromTokensUsed      int64   `json:"from_tokens_used"`
	FromTokensAvailable int64   `json:"from_tokens_available"`
	FromPercentLeft     float64 `json:"from_percent_left"`
	ToTokensUsed        int64   `json:"to_tokens_used"`
	ToTokensAvailable   int64   `json:"to_tokens_available"`
	ToPercentLeft       float64 `json:"to_percent_left"`
	ResponseID          string  `json:"response_id,omitempty"`
}

// FinalInfo carries session cursor and token accounting for a final message.
type FinalInfo struct {
	ResponseID         string `json:"response_id,omitempty"`
	PreviousResponseID string `json:"previous_response_id,omitempty"`
	RemainingTokens    *int64 `json:"remaining_tokens,omitempty"`
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
	if status.Compaction == nil {
		return Msg{}, false
	}
	used := status.Compaction.ToTokensUsed
	available := status.Compaction.ToTokensAvailable
	if available <= 0 {
		return Msg{}, false
	}
	return Msg{Type: MsgTypeContextUsage, Usage: newContextUsage(used, available, 0, 0)}, true
}

// newContextUsage builds a ContextUsage from raw token counts, computing
// PercentUsed and TokensAvailable once. window is the full context window.
func newContextUsage(inputUsed int64, window int64, outputUsed int64, totalUsed int64) *ContextUsage {
	usage := &ContextUsage{
		InputTokensUsed:  inputUsed,
		OutputTokensUsed: outputUsed,
		TotalTokensUsed:  totalUsed,
		ContextWindow:    window,
	}
	if window > 0 {
		remaining := window - inputUsed
		if remaining < 0 {
			remaining = 0
		}
		usage.TokensAvailable = remaining
		usage.PercentUsed = (float64(inputUsed) / float64(window)) * 100
	}
	return usage
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
