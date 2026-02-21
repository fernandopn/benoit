package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fernandopn/benoit/providers"
)

type commandProviderStub struct {
	chatCalls          int
	sessionCalls       int
	performCalls       int
	lastSessionID      string
	lastPrompt         string
	performErr         error
	performSummary     string
	capturedCompPrompt string
}

func (p *commandProviderStub) Chat(_ context.Context, input string) <-chan providers.Msg {
	p.chatCalls++
	p.lastPrompt = input
	return testMsgStream(providers.Msg{Type: providers.MsgTypeChatFinal, Value: "chat"})
}

func (p *commandProviderStub) ChatInSession(_ context.Context, input string, sessionID string) <-chan providers.Msg {
	p.sessionCalls++
	p.lastSessionID = sessionID
	p.lastPrompt = input
	return testMsgStream(providers.Msg{Type: providers.MsgTypeChatFinal, Value: "session chat"})
}

func (p *commandProviderStub) PerformCompression(ctx context.Context, sessionID string, compressor providers.Compressor) (string, error) {
	p.performCalls++
	p.lastSessionID = sessionID
	if p.performErr != nil {
		return "", p.performErr
	}
	if compressor != nil && p.performSummary == "" {
		capture := &promptCaptureProvider{}
		summary, err := compressor.Compress(ctx, capture, sessionID)
		p.capturedCompPrompt = capture.prompt
		if err != nil {
			return "", err
		}
		return summary, nil
	}
	return p.performSummary, nil
}

func (p *commandProviderStub) ListModels(_ context.Context) ([]string, error) {
	return nil, nil
}

func (p *commandProviderStub) Name() string {
	return "stub"
}

type promptCaptureProvider struct {
	prompt string
}

func (p *promptCaptureProvider) Chat(_ context.Context, input string) <-chan providers.Msg {
	p.prompt = input
	return testMsgStream(providers.Msg{Type: providers.MsgTypeChatFinal, Value: "summary"})
}

func (p *promptCaptureProvider) PerformCompression(_ context.Context, _ string, _ providers.Compressor) (string, error) {
	return "", errors.New("not implemented")
}

func (p *promptCaptureProvider) ListModels(_ context.Context) ([]string, error) {
	return nil, nil
}

func (p *promptCaptureProvider) Name() string {
	return "capture"
}

func testMsgStream(msgs ...providers.Msg) <-chan providers.Msg {
	out := make(chan providers.Msg, len(msgs))
	for _, msg := range msgs {
		out <- msg
	}
	close(out)
	return out
}

func collectMsgs(ch <-chan providers.Msg) []providers.Msg {
	out := make([]providers.Msg, 0)
	for msg := range ch {
		out = append(out, msg)
	}
	return out
}

func TestParseCompressCommand(t *testing.T) {
	maxWords, ok, err := parseCompressCommand("/compress")
	if err != nil || !ok || maxWords != defaultCompressionMaxWords {
		t.Fatalf("unexpected parse result: maxWords=%d ok=%v err=%v", maxWords, ok, err)
	}

	maxWords, ok, err = parseCompressCommand("/compress 77")
	if err != nil || !ok || maxWords != 77 {
		t.Fatalf("unexpected parse with explicit words: maxWords=%d ok=%v err=%v", maxWords, ok, err)
	}

	_, ok, err = parseCompressCommand("hello")
	if err != nil || ok {
		t.Fatalf("expected non-command prompt, got ok=%v err=%v", ok, err)
	}

	_, ok, err = parseCompressCommand("/compress nope")
	if !ok || err == nil {
		t.Fatalf("expected command parse error, got ok=%v err=%v", ok, err)
	}
}

func TestStreamStartForProviderUsesCompressionCommand(t *testing.T) {
	provider := &commandProviderStub{performSummary: "compressed summary"}
	start := streamStartForProvider(provider, "session-42")
	msgs := collectMsgs(start(context.Background(), "/compress"))

	if provider.performCalls != 1 {
		t.Fatalf("expected one compression call, got %d", provider.performCalls)
	}
	if provider.chatCalls != 0 || provider.sessionCalls != 0 {
		t.Fatalf("expected no chat calls for /compress, got chat=%d session=%d", provider.chatCalls, provider.sessionCalls)
	}
	if provider.lastSessionID != "session-42" {
		t.Fatalf("unexpected session ID: %q", provider.lastSessionID)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 stream messages, got %d", len(msgs))
	}
	if msgs[0].Type != providers.MsgTypeChatDelta || msgs[0].Value != "compressed summary" {
		t.Fatalf("unexpected first message: %#v", msgs[0])
	}
	if msgs[1].Type != providers.MsgTypeChatFinal || msgs[1].Value != "compressed summary" {
		t.Fatalf("unexpected second message: %#v", msgs[1])
	}
}

func TestStreamStartForProviderCompressParsesWordLimit(t *testing.T) {
	provider := &commandProviderStub{}
	start := streamStartForProvider(provider, "")
	msgs := collectMsgs(start(context.Background(), "/compress 123"))

	if provider.performCalls != 1 {
		t.Fatalf("expected one compression call, got %d", provider.performCalls)
	}
	if !strings.Contains(provider.capturedCompPrompt, "at most 123 words") {
		t.Fatalf("unexpected compression prompt: %q", provider.capturedCompPrompt)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 command messages, got %d", len(msgs))
	}
}

func TestStreamStartForProviderCompressError(t *testing.T) {
	provider := &commandProviderStub{performErr: errors.New("boom")}
	start := streamStartForProvider(provider, "")
	msgs := collectMsgs(start(context.Background(), "/compress"))

	if len(msgs) != 1 {
		t.Fatalf("expected a single error message, got %d", len(msgs))
	}
	if msgs[0].Type != providers.MsgTypeError || msgs[0].Value != "boom" {
		t.Fatalf("unexpected error message: %#v", msgs[0])
	}
}

func TestStreamStartForProviderUsesSessionChatForPrompts(t *testing.T) {
	provider := &commandProviderStub{}
	start := streamStartForProvider(provider, "abc")
	msgs := collectMsgs(start(context.Background(), "hello"))

	if provider.performCalls != 0 {
		t.Fatalf("did not expect compression call, got %d", provider.performCalls)
	}
	if provider.sessionCalls != 1 || provider.chatCalls != 0 {
		t.Fatalf("expected one session chat and no direct chat, got session=%d chat=%d", provider.sessionCalls, provider.chatCalls)
	}
	if provider.lastSessionID != "abc" {
		t.Fatalf("unexpected session ID: %q", provider.lastSessionID)
	}
	if len(msgs) != 1 || msgs[0].Value != "session chat" {
		t.Fatalf("unexpected chat result: %#v", msgs)
	}
}
