package compression

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fernandopn/benoit/providers"
)

var (
	errContextRequired          = errors.New("context is required")
	errProviderRequired         = errors.New("provider is required")
	errMaxWordsMustBePositive   = errors.New("maxWords must be greater than zero")
	errProviderStreamNil        = errors.New("provider returned a nil stream")
	errProviderCompressionEmpty = errors.New("provider returned empty compression")
)

// Basic is a compressor that summarizes conversation context with a
// configurable word budget.
type Basic struct {
	MaxWords int
}

var _ providers.Compressor = Basic{}

func NewBasic(maxWords int) Basic {
	return Basic{MaxWords: maxWords}
}

// BasicCompression asks the provided LLM backend to summarize the current
// conversation context into a smaller form.
func BasicCompression(ctx context.Context, provider providers.Provider, sessionID string, maxWords int) (string, error) {
	return NewBasic(maxWords).Compress(ctx, provider, sessionID)
}

// Compress asks the provided LLM backend to summarize the current
// conversation context into a smaller form.
func (b Basic) Compress(ctx context.Context, provider providers.Provider, sessionID string) (string, error) {
	if ctx == nil {
		return "", errContextRequired
	}
	if provider == nil {
		return "", errProviderRequired
	}

	maxWords := b.MaxWords
	if maxWords <= 0 {
		return "", errMaxWordsMustBePositive
	}

	prompt := basicCompressionPrompt(maxWords)
	stream := startCompressionStream(ctx, provider, strings.TrimSpace(sessionID), prompt)
	if stream == nil {
		return "", errProviderStreamNil
	}

	var (
		chatDelta strings.Builder
		chatFinal strings.Builder
	)
	for msg := range stream {
		switch msg.Type {
		case providers.MsgTypeChatDelta:
			chatDelta.WriteString(msg.Value)
		case providers.MsgTypeChatFinal:
			chatFinal.WriteString(msg.Value)
		case providers.MsgTypeError:
			errText := strings.TrimSpace(msg.Value)
			if errText == "" {
				errText = "provider returned an empty error"
			}
			return "", errors.New(errText)
		}
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	compressed := strings.TrimSpace(chatFinal.String())
	if compressed == "" {
		compressed = strings.TrimSpace(chatDelta.String())
	}
	if compressed == "" {
		return "", errProviderCompressionEmpty
	}

	normalized := strings.Join(strings.Fields(compressed), " ")
	return normalized, nil
}

func startCompressionStream(ctx context.Context, provider providers.Provider, sessionID string, prompt string) <-chan providers.Msg {
	_ = sessionID
	return provider.Chat(ctx, prompt)
}

func basicCompressionPrompt(maxWords int) string {
	return fmt.Sprintf(
		"We need to continue this conversation with less context. "+
			"Explain the current conversation context in detail, including user goals, constraints, decisions made, tool outcomes, pending questions, and next actions. "+
			"Keep it concise and self-contained. Return only plain text and target at most %d words.",
		maxWords,
	)
}
