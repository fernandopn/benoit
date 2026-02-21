package middleware

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/fernandopn/benoit/providers"
)

const defaultCompressionBarrierSessionID = "__default__"

type CompressionBarrier struct {
	provider         providers.Provider
	mu               sync.RWMutex
	blockedBySession map[string]bool
}

var _ providers.Provider = (*CompressionBarrier)(nil)
var _ providers.SessionProvider = (*CompressionBarrier)(nil)

func NewCompressionBarrier(provider providers.Provider) (*CompressionBarrier, error) {
	if provider == nil {
		return nil, errors.New("provider is required")
	}
	return &CompressionBarrier{
		provider:         provider,
		blockedBySession: map[string]bool{},
	}, nil
}

func (c *CompressionBarrier) Chat(ctx context.Context, input string) <-chan providers.Msg {
	return c.wrapStream(defaultCompressionBarrierSessionID, c.provider.Chat(ctx, input))
}

func (c *CompressionBarrier) ChatInSession(ctx context.Context, input string, sessionID string) <-chan providers.Msg {
	sessionProvider, ok := c.provider.(providers.SessionProvider)
	if !ok {
		return c.Chat(ctx, input)
	}
	return c.wrapStream(sessionID, sessionProvider.ChatInSession(ctx, input, sessionID))
}

func (c *CompressionBarrier) PerformCompression(ctx context.Context, sessionID string, compressor providers.Compressor) (string, error) {
	normalizedSessionID := normalizeCompressionBarrierSessionID(sessionID)
	c.setBlocked(normalizedSessionID, true)
	summary, err := c.provider.PerformCompression(ctx, sessionID, compressor)
	if err != nil {
		c.setBlocked(normalizedSessionID, false)
		return "", err
	}
	return summary, nil
}

func (c *CompressionBarrier) NotifyCompressionStatusSent(sessionID string) {
	c.setBlocked(normalizeCompressionBarrierSessionID(sessionID), false)
}

func (c *CompressionBarrier) ListModels(ctx context.Context) ([]string, error) {
	return c.provider.ListModels(ctx)
}

func (c *CompressionBarrier) Name() string {
	return c.provider.Name()
}

func (c *CompressionBarrier) wrapStream(sessionID string, in <-chan providers.Msg) <-chan providers.Msg {
	if in == nil {
		return nil
	}
	normalizedSessionID := normalizeCompressionBarrierSessionID(sessionID)
	out := make(chan providers.Msg)
	go func() {
		defer close(out)
		for msg := range in {
			if msg.Type == providers.MsgTypeCompressionStatus {
				c.setBlocked(normalizedSessionID, false)
				out <- msg
				continue
			}
			if c.isBlocked(normalizedSessionID) && isFinalOrDeltaMessage(msg.Type) {
				continue
			}
			out <- msg
		}
	}()
	return out
}

func (c *CompressionBarrier) setBlocked(sessionID string, blocked bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.blockedBySession == nil {
		c.blockedBySession = map[string]bool{}
	}
	if blocked {
		c.blockedBySession[sessionID] = true
		return
	}
	delete(c.blockedBySession, sessionID)
}

func (c *CompressionBarrier) isBlocked(sessionID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.blockedBySession == nil {
		return false
	}
	return c.blockedBySession[sessionID]
}

func isFinalOrDeltaMessage(msgType providers.MsgType) bool {
	switch msgType {
	case providers.MsgTypeChatDelta,
		providers.MsgTypeChatFinal,
		providers.MsgTypeReasoningSummaryDelta,
		providers.MsgTypeReasoningSummaryFinal:
		return true
	default:
		return false
	}
}

func normalizeCompressionBarrierSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return defaultCompressionBarrierSessionID
	}
	return sessionID
}
