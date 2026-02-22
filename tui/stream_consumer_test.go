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
	notifyCalls        int
	lastSessionID      string
	notifySessionID    string
	lastPrompt         string
	performErr         error
	performSummary     string
	capturedCompPrompt string
	hasCompressionStat bool
	compressionStat    providers.Msg
}

func (p *commandProviderStub) Chat(ctx context.Context, input string) <-chan providers.Msg {
	p.chatCalls++
	if sessionID := providers.SessionIDFromContext(ctx); sessionID != "" {
		p.sessionCalls++
		p.lastSessionID = sessionID
	}
	p.lastPrompt = input
	if p.lastSessionID != "" {
		return testMsgStream(providers.Msg{Type: providers.MsgTypeChatFinal, Value: "session chat"})
	}
	return testMsgStream(providers.Msg{Type: providers.MsgTypeChatFinal, Value: "chat"})
}

func (p *commandProviderStub) PerformCompression(ctx context.Context, sessionID string, compressor providers.Compressor) (string, error) {
	p.performCalls++
	p.lastSessionID = sessionID
	if p.performErr != nil {
		return "", p.performErr
	}
	if p.hasCompressionStat {
		providers.SetCompressionStatus(ctx, p.compressionStat)
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

func (p *commandProviderStub) NotifyCompressionStatusSent(sessionID string) {
	p.notifyCalls++
	p.notifySessionID = sessionID
}

type promptCaptureProvider struct {
	prompt string
}

func (p *promptCaptureProvider) Chat(_ context.Context, input string) <-chan providers.Msg {
	p.prompt = input
	return testMsgStream(
		providers.Msg{
			Type: providers.MsgTypeContextUsage,
			Metadata: map[string]string{
				"tokens_input_used": "24000",
				"tokens_available":  "400000",
			},
		},
		providers.Msg{Type: providers.MsgTypeChatFinal, Value: "summary"},
	)
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

func TestParseCompactCommand(t *testing.T) {
	maxWords, ok, err := parseCompactCommand("/compact")
	if err != nil || !ok || maxWords != defaultCompressionMaxWords {
		t.Fatalf("unexpected parse result: maxWords=%d ok=%v err=%v", maxWords, ok, err)
	}

	maxWords, ok, err = parseCompactCommand("/compact 77")
	if err != nil || !ok || maxWords != 77 {
		t.Fatalf("unexpected parse with explicit words: maxWords=%d ok=%v err=%v", maxWords, ok, err)
	}

	_, ok, err = parseCompactCommand("hello")
	if err != nil || ok {
		t.Fatalf("expected non-command prompt, got ok=%v err=%v", ok, err)
	}

	_, ok, err = parseCompactCommand("/compact nope")
	if !ok || err == nil {
		t.Fatalf("expected command parse error, got ok=%v err=%v", ok, err)
	}
}

func TestStreamStartForProviderWithCustomParser(t *testing.T) {
	provider := &commandProviderStub{}
	parserCalls := 0
	start := streamStartForProviderWithCommandParser(provider, "session-42", func(prompt string) (int, bool, error) {
		parserCalls++
		if strings.TrimSpace(prompt) == "/compact" {
			return 0, true, nil
		}
		return 0, false, nil
	})

	msgs := collectMsgs(start(context.Background(), "/compact"))
	if parserCalls != 1 {
		t.Fatalf("expected parser call for compact prompt, got %d", parserCalls)
	}
	if provider.performCalls != 1 {
		t.Fatalf("expected compression branch, got %d calls", provider.performCalls)
	}
	if provider.chatCalls != 0 || provider.sessionCalls != 0 {
		t.Fatalf("expected no chat call for compact command, got chat=%d session=%d", provider.chatCalls, provider.sessionCalls)
	}
	if len(msgs) == 0 {
		t.Fatalf("expected compression stream messages")
	}

	provider = &commandProviderStub{}
	parserCalls = 0
	start = streamStartForProviderWithCommandParser(provider, "session-42", func(prompt string) (int, bool, error) {
		parserCalls++
		return 0, false, nil
	})
	msgs = collectMsgs(start(context.Background(), "hello"))
	if parserCalls != 1 {
		t.Fatalf("expected parser call for normal prompt, got %d", parserCalls)
	}
	if provider.performCalls != 0 {
		t.Fatalf("expected no compression for non-command prompt, got %d calls", provider.performCalls)
	}
	if provider.chatCalls != 1 {
		t.Fatalf("expected chat call, got %d", provider.chatCalls)
	}
	if len(msgs) != 1 || msgs[0].Value != "session chat" {
		t.Fatalf("unexpected stream messages: %#v", msgs)
	}
}

func TestStreamStartForProviderUsesCompressionCommand(t *testing.T) {
	provider := &commandProviderStub{
		performSummary:     "compressed summary",
		hasCompressionStat: true,
		compressionStat: providers.Msg{
			Type:  providers.MsgTypeCompressionStatus,
			Value: "Context compressed from 43200 (89.2% left) to 21000 (94.8% left).",
			Metadata: map[string]string{
				"from_tokens_used":      "43200",
				"to_tokens_used":        "21000",
				"to_tokens_available":   "400000",
				"to_left_percent":       "94.8",
				"from_tokens_available": "400000",
			},
		},
	}
	start := streamStartForProvider(provider, "session-42")
	msgs := collectMsgs(start(context.Background(), "/compact"))

	if provider.performCalls != 1 {
		t.Fatalf("expected one compression call, got %d", provider.performCalls)
	}
	if provider.chatCalls != 0 || provider.sessionCalls != 0 {
		t.Fatalf("expected no chat calls for /compact, got chat=%d session=%d", provider.chatCalls, provider.sessionCalls)
	}
	if provider.lastSessionID != "session-42" {
		t.Fatalf("unexpected session ID: %q", provider.lastSessionID)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected 4 stream messages, got %d", len(msgs))
	}
	if msgs[0].Type != providers.MsgTypeCompressionStatus || msgs[0].Value != "Context compressed from 43200 (89.2% left) to 21000 (94.8% left)." {
		t.Fatalf("unexpected first message: %#v", msgs[0])
	}
	if msgs[0].Metadata["from_tokens_used"] != "43200" || msgs[0].Metadata["to_tokens_used"] != "21000" {
		t.Fatalf("unexpected compression status metadata: %#v", msgs[0].Metadata)
	}
	if msgs[1].Type != providers.MsgTypeContextUsage {
		t.Fatalf("unexpected second message: %#v", msgs[1])
	}
	if msgs[1].Metadata["tokens_input_used"] != "21000" || msgs[1].Metadata["tokens_available"] != "400000" {
		t.Fatalf("unexpected context usage metadata: %#v", msgs[1].Metadata)
	}
	if msgs[2].Type != providers.MsgTypeChatDelta || msgs[2].Value != "compressed summary" {
		t.Fatalf("unexpected third message: %#v", msgs[2])
	}
	if msgs[3].Type != providers.MsgTypeChatFinal || msgs[3].Value != "compressed summary" {
		t.Fatalf("unexpected fourth message: %#v", msgs[3])
	}
	if provider.notifyCalls != 1 || provider.notifySessionID != "session-42" {
		t.Fatalf("expected compression status notification for session-42, got calls=%d session=%q", provider.notifyCalls, provider.notifySessionID)
	}
}

func TestStreamStartForProviderCompressParsesWordLimit(t *testing.T) {
	provider := &commandProviderStub{}
	start := streamStartForProvider(provider, "")
	msgs := collectMsgs(start(context.Background(), "/compact 123"))

	if provider.performCalls != 1 {
		t.Fatalf("expected one compression call, got %d", provider.performCalls)
	}
	if !strings.Contains(provider.capturedCompPrompt, "at most 123 words") {
		t.Fatalf("unexpected compression prompt: %q", provider.capturedCompPrompt)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected 4 command messages, got %d", len(msgs))
	}
	if msgs[0].Type != providers.MsgTypeCompressionStatus || msgs[0].Value != compressionFinishedMessage {
		t.Fatalf("unexpected first command message: %#v", msgs[0])
	}
	if msgs[1].Type != providers.MsgTypeContextUsage {
		t.Fatalf("unexpected second command message: %#v", msgs[1])
	}
	if msgs[1].Metadata["tokens_input_used"] != "24000" || msgs[1].Metadata["tokens_available"] != "400000" {
		t.Fatalf("unexpected second command metadata: %#v", msgs[1].Metadata)
	}
	if msgs[2].Type != providers.MsgTypeChatDelta || msgs[2].Value != "summary" {
		t.Fatalf("unexpected third command message: %#v", msgs[2])
	}
	if msgs[3].Type != providers.MsgTypeChatFinal || msgs[3].Value != "summary" {
		t.Fatalf("unexpected fourth command message: %#v", msgs[3])
	}
	if provider.notifyCalls != 1 || provider.notifySessionID != "" {
		t.Fatalf("expected compression status notification for default session, got calls=%d session=%q", provider.notifyCalls, provider.notifySessionID)
	}
}

func TestStreamStartForProviderCompressError(t *testing.T) {
	provider := &commandProviderStub{performErr: errors.New("boom")}
	start := streamStartForProvider(provider, "")
	msgs := collectMsgs(start(context.Background(), "/compact"))

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
	if provider.sessionCalls != 1 || provider.chatCalls != 1 {
		t.Fatalf("expected one session-routed chat call, got session=%d chat=%d", provider.sessionCalls, provider.chatCalls)
	}
	if provider.lastSessionID != "abc" {
		t.Fatalf("unexpected session ID: %q", provider.lastSessionID)
	}
	if len(msgs) != 1 || msgs[0].Value != "session chat" {
		t.Fatalf("unexpected chat result: %#v", msgs)
	}
}
