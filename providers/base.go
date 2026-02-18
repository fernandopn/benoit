package providers

import "context"

type MsgType int

const (
	MsgTypeChat MsgType = iota
	MsgTypeError
)

// Msg represents a message emitted by a provider.
type Msg struct {
	Type  MsgType
	Value string
}

// Provider abstracts the chat interaction for a model backend.
type Provider interface {
	// Chat returns a stream of messages. The stream is done when the channel closes.
	Chat(ctx context.Context, input string) <-chan Msg
	// ListModels returns the available model IDs for the provider.
	ListModels(ctx context.Context) ([]string, error)
	// Name returns the provider display name.
	Name() string
}
