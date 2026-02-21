package compression

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fernandopn/benoit/providers"
)

type providerStub struct {
	chatFunc   func(context.Context, string) <-chan providers.Msg
	chatInputs []string
}

func (p *providerStub) Chat(ctx context.Context, input string) <-chan providers.Msg {
	p.chatInputs = append(p.chatInputs, input)
	if p.chatFunc == nil {
		return nil
	}
	return p.chatFunc(ctx, input)
}

func (p *providerStub) PerformCompression(ctx context.Context, sessionID string, compressor providers.Compressor) (string, error) {
	_ = ctx
	_ = sessionID
	_ = compressor
	return "", errors.New("not implemented")
}

func (p *providerStub) ListModels(ctx context.Context) ([]string, error) {
	_ = ctx
	return nil, nil
}

func (p *providerStub) Name() string {
	return "stub"
}

func msgStream(msgs ...providers.Msg) <-chan providers.Msg {
	out := make(chan providers.Msg, len(msgs))
	for _, msg := range msgs {
		out <- msg
	}
	close(out)
	return out
}

func TestBasicCompressionValidation(t *testing.T) {
	provider := &providerStub{chatFunc: func(context.Context, string) <-chan providers.Msg {
		return msgStream()
	}}

	_, err := BasicCompression(nil, provider, "", 10)
	if !errors.Is(err, errContextRequired) {
		t.Fatalf("expected errContextRequired, got %v", err)
	}

	_, err = BasicCompression(context.Background(), nil, "", 10)
	if !errors.Is(err, errProviderRequired) {
		t.Fatalf("expected errProviderRequired, got %v", err)
	}

	_, err = BasicCompression(context.Background(), provider, "", 0)
	if !errors.Is(err, errMaxWordsMustBePositive) {
		t.Fatalf("expected errMaxWordsMustBePositive, got %v", err)
	}
}

func TestBasicCompressionUsesProviderChat(t *testing.T) {
	provider := &providerStub{chatFunc: func(context.Context, string) <-chan providers.Msg {
		return msgStream(providers.Msg{Type: providers.MsgTypeChatFinal, Value: "ok"})
	}}

	got, err := BasicCompression(context.Background(), provider, "unused-session", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("expected compressed output %q, got %q", "ok", got)
	}
	if len(provider.chatInputs) != 1 {
		t.Fatalf("expected one Chat call, got %d", len(provider.chatInputs))
	}
	if provider.chatInputs[0] != basicCompressionPrompt(42) {
		t.Fatalf("unexpected chat prompt: %q", provider.chatInputs[0])
	}
}

func TestBasicCompressionReturnsErrorWhenStreamIsNil(t *testing.T) {
	provider := &providerStub{chatFunc: func(context.Context, string) <-chan providers.Msg {
		return nil
	}}

	_, err := BasicCompression(context.Background(), provider, "", 10)
	if !errors.Is(err, errProviderStreamNil) {
		t.Fatalf("expected errProviderStreamNil, got %v", err)
	}
}

func TestBasicCompressionPrefersFinalOverDelta(t *testing.T) {
	provider := &providerStub{chatFunc: func(context.Context, string) <-chan providers.Msg {
		return msgStream(
			providers.Msg{Type: providers.MsgTypeChatDelta, Value: "from delta"},
			providers.Msg{Type: providers.MsgTypeChatFinal, Value: "  from \n final  "},
		)
	}}

	got, err := BasicCompression(context.Background(), provider, "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "from final" {
		t.Fatalf("expected final text to win, got %q", got)
	}
}

func TestBasicCompressionFallsBackToDelta(t *testing.T) {
	provider := &providerStub{chatFunc: func(context.Context, string) <-chan providers.Msg {
		return msgStream(
			providers.Msg{Type: providers.MsgTypeChatDelta, Value: "  from "},
			providers.Msg{Type: providers.MsgTypeChatDelta, Value: "\n delta  "},
		)
	}}

	got, err := BasicCompression(context.Background(), provider, "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "from delta" {
		t.Fatalf("expected delta fallback, got %q", got)
	}
}

func TestBasicCompressionProviderError(t *testing.T) {
	provider := &providerStub{chatFunc: func(context.Context, string) <-chan providers.Msg {
		return msgStream(providers.Msg{Type: providers.MsgTypeError, Value: "  boom  "})
	}}

	_, err := BasicCompression(context.Background(), provider, "", 10)
	if err == nil {
		t.Fatal("expected provider error")
	}
	if err.Error() != "boom" {
		t.Fatalf("expected trimmed provider error, got %q", err.Error())
	}
}

func TestBasicCompressionProviderEmptyError(t *testing.T) {
	provider := &providerStub{chatFunc: func(context.Context, string) <-chan providers.Msg {
		return msgStream(providers.Msg{Type: providers.MsgTypeError, Value: "   "})
	}}

	_, err := BasicCompression(context.Background(), provider, "", 10)
	if err == nil {
		t.Fatal("expected provider error")
	}
	if err.Error() != "provider returned an empty error" {
		t.Fatalf("unexpected provider error: %q", err.Error())
	}
}

func TestBasicCompressionEmptyCompression(t *testing.T) {
	provider := &providerStub{chatFunc: func(context.Context, string) <-chan providers.Msg {
		return msgStream(
			providers.Msg{Type: providers.MsgTypeChatDelta, Value: "  \n "},
			providers.Msg{Type: providers.MsgTypeChatFinal, Value: "\t  "},
		)
	}}

	_, err := BasicCompression(context.Background(), provider, "", 10)
	if !errors.Is(err, errProviderCompressionEmpty) {
		t.Fatalf("expected errProviderCompressionEmpty, got %v", err)
	}
}

func TestBasicCompressionContextErrorWinsAfterStreamEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &providerStub{chatFunc: func(context.Context, string) <-chan providers.Msg {
		ch := make(chan providers.Msg, 1)
		ch <- providers.Msg{Type: providers.MsgTypeChatFinal, Value: "will be ignored"}
		cancel()
		close(ch)
		return ch
	}}

	_, err := BasicCompression(ctx, provider, "", 10)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestBasicCompressionPromptIncludesWordLimit(t *testing.T) {
	prompt := basicCompressionPrompt(77)
	if !strings.Contains(prompt, "at most 77 words") {
		t.Fatalf("expected prompt to include max words, got %q", prompt)
	}
	if !strings.Contains(prompt, "user goals") {
		t.Fatalf("expected prompt to include compression instructions, got %q", prompt)
	}
}
