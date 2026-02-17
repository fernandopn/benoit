package providers

import (
	"context"
	"io"
)

// Provider abstracts the chat interaction for a model backend.
type Provider interface {
	// Chat writes the assistant response to w and returns the new previous response ID.
	Chat(ctx context.Context, input string, previousID string, w io.Writer) (string, error)
}
