package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/fernandopn/benoit/providers"
)

type compressionBarrierStubProvider struct {
	chatMsgs       []providers.Msg
	sessionMsgs    map[string][]providers.Msg
	performSummary string
	performErr     error
}

func (s *compressionBarrierStubProvider) Chat(_ context.Context, _ string) <-chan providers.Msg {
	return compressionBarrierStream(s.chatMsgs...)
}

func (s *compressionBarrierStubProvider) ChatInSession(_ context.Context, _ string, sessionID string) <-chan providers.Msg {
	if msgs, ok := s.sessionMsgs[sessionID]; ok {
		return compressionBarrierStream(msgs...)
	}
	return compressionBarrierStream(s.chatMsgs...)
}

func (s *compressionBarrierStubProvider) PerformCompression(_ context.Context, _ string, _ providers.Compressor) (string, error) {
	if s.performErr != nil {
		return "", s.performErr
	}
	return s.performSummary, nil
}

func (s *compressionBarrierStubProvider) ListModels(_ context.Context) ([]string, error) {
	return nil, nil
}

func (s *compressionBarrierStubProvider) Name() string {
	return "stub"
}

func compressionBarrierStream(msgs ...providers.Msg) <-chan providers.Msg {
	out := make(chan providers.Msg, len(msgs))
	for _, msg := range msgs {
		out <- msg
	}
	close(out)
	return out
}

func collectBarrierMsgs(ch <-chan providers.Msg) []providers.Msg {
	out := make([]providers.Msg, 0)
	for msg := range ch {
		out = append(out, msg)
	}
	return out
}

func TestCompressionBarrierPassThroughWhenNotBlocked(t *testing.T) {
	base := &compressionBarrierStubProvider{chatMsgs: []providers.Msg{
		{Type: providers.MsgTypeChatDelta, Value: "hel"},
		{Type: providers.MsgTypeChatFinal, Value: "hello"},
		{Type: providers.MsgTypeToolResult, Value: "ok"},
	}}
	barrier, err := NewCompressionBarrier(base)
	if err != nil {
		t.Fatalf("new barrier: %v", err)
	}

	got := collectBarrierMsgs(barrier.Chat(context.Background(), "hi"))
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	if got[0].Type != providers.MsgTypeChatDelta || got[1].Type != providers.MsgTypeChatFinal || got[2].Type != providers.MsgTypeToolResult {
		t.Fatalf("unexpected forwarded messages: %#v", got)
	}
}

func TestCompressionBarrierBlocksFinalAndDeltaAfterCompression(t *testing.T) {
	base := &compressionBarrierStubProvider{sessionMsgs: map[string][]providers.Msg{
		"session-1": {
			{Type: providers.MsgTypeChatDelta, Value: "hidden"},
			{Type: providers.MsgTypeReasoningSummaryFinal, Value: "hidden reasoning"},
			{Type: providers.MsgTypeToolResult, Value: "visible"},
		},
	}}
	barrier, err := NewCompressionBarrier(base)
	if err != nil {
		t.Fatalf("new barrier: %v", err)
	}

	if _, err := barrier.PerformCompression(context.Background(), "session-1", nil); err != nil {
		t.Fatalf("perform compression: %v", err)
	}

	got := collectBarrierMsgs(barrier.ChatInSession(context.Background(), "hello", "session-1"))
	if len(got) != 1 {
		t.Fatalf("expected only one non-final/delta message, got %d", len(got))
	}
	if got[0].Type != providers.MsgTypeToolResult || got[0].Value != "visible" {
		t.Fatalf("unexpected forwarded message: %#v", got[0])
	}
}

func TestCompressionBarrierUnblocksWhenStatusSent(t *testing.T) {
	base := &compressionBarrierStubProvider{sessionMsgs: map[string][]providers.Msg{
		"session-1": {
			{Type: providers.MsgTypeChatDelta, Value: "visible"},
			{Type: providers.MsgTypeChatFinal, Value: "visible"},
		},
	}}
	barrier, err := NewCompressionBarrier(base)
	if err != nil {
		t.Fatalf("new barrier: %v", err)
	}

	if _, err := barrier.PerformCompression(context.Background(), "session-1", nil); err != nil {
		t.Fatalf("perform compression: %v", err)
	}
	barrier.NotifyCompressionStatusSent("session-1")

	got := collectBarrierMsgs(barrier.ChatInSession(context.Background(), "hello", "session-1"))
	if len(got) != 2 {
		t.Fatalf("expected 2 forwarded messages, got %d", len(got))
	}
	if got[0].Type != providers.MsgTypeChatDelta || got[1].Type != providers.MsgTypeChatFinal {
		t.Fatalf("unexpected forwarded messages: %#v", got)
	}
}

func TestCompressionBarrierStatusMessageInStreamUnblocks(t *testing.T) {
	base := &compressionBarrierStubProvider{sessionMsgs: map[string][]providers.Msg{
		"session-1": {
			{Type: providers.MsgTypeCompressionStatus, Value: "done"},
			{Type: providers.MsgTypeChatDelta, Value: "visible"},
			{Type: providers.MsgTypeChatFinal, Value: "visible"},
		},
	}}
	barrier, err := NewCompressionBarrier(base)
	if err != nil {
		t.Fatalf("new barrier: %v", err)
	}

	if _, err := barrier.PerformCompression(context.Background(), "session-1", nil); err != nil {
		t.Fatalf("perform compression: %v", err)
	}

	got := collectBarrierMsgs(barrier.ChatInSession(context.Background(), "hello", "session-1"))
	if len(got) != 3 {
		t.Fatalf("expected 3 forwarded messages, got %d", len(got))
	}
	if got[0].Type != providers.MsgTypeCompressionStatus || got[1].Type != providers.MsgTypeChatDelta || got[2].Type != providers.MsgTypeChatFinal {
		t.Fatalf("unexpected forwarded messages: %#v", got)
	}
}

func TestCompressionBarrierCompressionErrorClearsBlock(t *testing.T) {
	base := &compressionBarrierStubProvider{
		chatMsgs:   []providers.Msg{{Type: providers.MsgTypeChatFinal, Value: "visible"}},
		performErr: errors.New("boom"),
	}
	barrier, err := NewCompressionBarrier(base)
	if err != nil {
		t.Fatalf("new barrier: %v", err)
	}

	if _, err := barrier.PerformCompression(context.Background(), "session-1", nil); err == nil {
		t.Fatal("expected compression error")
	}

	got := collectBarrierMsgs(barrier.Chat(context.Background(), "hello"))
	if len(got) != 1 || got[0].Type != providers.MsgTypeChatFinal {
		t.Fatalf("expected chat final after failed compression, got %#v", got)
	}
}

func TestCompressionBarrierSessionIsolation(t *testing.T) {
	base := &compressionBarrierStubProvider{sessionMsgs: map[string][]providers.Msg{
		"session-a": {{Type: providers.MsgTypeChatFinal, Value: "hidden"}},
		"session-b": {{Type: providers.MsgTypeChatFinal, Value: "visible"}},
	}}
	barrier, err := NewCompressionBarrier(base)
	if err != nil {
		t.Fatalf("new barrier: %v", err)
	}

	if _, err := barrier.PerformCompression(context.Background(), "session-a", nil); err != nil {
		t.Fatalf("perform compression: %v", err)
	}

	blocked := collectBarrierMsgs(barrier.ChatInSession(context.Background(), "hello", "session-a"))
	if len(blocked) != 0 {
		t.Fatalf("expected no forwarded final/delta messages for blocked session, got %#v", blocked)
	}

	visible := collectBarrierMsgs(barrier.ChatInSession(context.Background(), "hello", "session-b"))
	if len(visible) != 1 || visible[0].Value != "visible" {
		t.Fatalf("unexpected session-b messages: %#v", visible)
	}
}
