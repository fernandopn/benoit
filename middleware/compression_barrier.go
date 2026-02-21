package middleware

import (
	"context"
	"errors"
	"sync"

	"github.com/fernandopn/benoit/providers"
)

type CompressionBarrier struct {
	provider providers.Provider
	mu       sync.RWMutex
	blocked  bool
}

var _ providers.Provider = (*CompressionBarrier)(nil)

func NewCompressionBarrier(provider providers.Provider) (*CompressionBarrier, error) {
	if provider == nil {
		return nil, errors.New("provider is required")
	}
	return &CompressionBarrier{provider: provider}, nil
}

func (c *CompressionBarrier) Chat(ctx context.Context, input string) <-chan providers.Msg {
	return c.wrapStream(c.provider.Chat(ctx, input))
}

func (c *CompressionBarrier) PerformCompression(ctx context.Context, sessionID string, compressor providers.Compressor) (string, error) {
	_ = sessionID
	c.setBlocked(true)
	summary, err := c.provider.PerformCompression(ctx, sessionID, compressor)
	if err != nil {
		c.setBlocked(false)
		return "", err
	}
	return summary, nil
}

func (c *CompressionBarrier) NotifyCompressionStatusSent(sessionID string) {
	_ = sessionID
	c.setBlocked(false)
}

func (c *CompressionBarrier) ListModels(ctx context.Context) ([]string, error) {
	return c.provider.ListModels(ctx)
}

func (c *CompressionBarrier) Name() string {
	return c.provider.Name()
}

func (c *CompressionBarrier) wrapStream(in <-chan providers.Msg) <-chan providers.Msg {
	if in == nil {
		return nil
	}
	out := make(chan providers.Msg)
	go func() {
		defer close(out)
		for msg := range in {
			if msg.Type == providers.MsgTypeCompressionStatus {
				c.setBlocked(false)
				out <- msg
				continue
			}
			if c.isBlocked() && isFinalOrDeltaMessage(msg.Type) {
				continue
			}
			out <- msg
		}
	}()
	return out
}

func (c *CompressionBarrier) setBlocked(blocked bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blocked = blocked
}

func (c *CompressionBarrier) isBlocked() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.blocked
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
