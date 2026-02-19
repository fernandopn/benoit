package tui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fernandopn/benoid/channels"
	"github.com/fernandopn/benoid/providers"
)

type telegramProviderStub struct {
	mu      sync.Mutex
	inputs  []string
	outputs []providers.Msg
}

func (p *telegramProviderStub) Chat(ctx context.Context, input string) <-chan providers.Msg {
	p.mu.Lock()
	p.inputs = append(p.inputs, input)
	outputs := append([]providers.Msg(nil), p.outputs...)
	p.mu.Unlock()

	out := make(chan providers.Msg, len(outputs))
	go func() {
		defer close(out)
		for _, msg := range outputs {
			select {
			case <-ctx.Done():
				out <- providers.Msg{Type: providers.MsgTypeError, Value: ctx.Err().Error()}
				return
			case out <- msg:
			}
		}
	}()
	return out
}

func (p *telegramProviderStub) ListModels(ctx context.Context) ([]string, error) {
	_ = ctx
	return nil, nil
}

func (p *telegramProviderStub) Name() string {
	return "telegram-provider"
}

func TestRunTelegramAggregatesChatAndReplies(t *testing.T) {
	provider := &telegramProviderStub{outputs: []providers.Msg{
		{Type: providers.MsgTypeReasoningSummary, Value: "internal"},
		{Type: providers.MsgTypeToolCall, Value: `{"tool":"clock"}`},
		{Type: providers.MsgTypeToolResult, Value: "2026-01-01T00:00:00Z"},
		{Type: providers.MsgTypeChat, Value: "Hello"},
		{Type: providers.MsgTypeChat, Value: " from bot"},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu           sync.Mutex
		updatesCalls int
		typingCalls  int
		sentTexts    []string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		switch r.URL.Path {
		case "/bottest-token/getUpdates":
			mu.Lock()
			updatesCalls++
			call := updatesCalls
			mu.Unlock()

			if call == 1 {
				response := map[string]any{
					"ok": true,
					"result": []map[string]any{
						{
							"update_id": 10,
							"message": map[string]any{
								"message_id": 20,
								"text":       "Who are you?",
								"chat": map[string]any{
									"id":   99,
									"type": "private",
								},
								"from": map[string]any{
									"id":     77,
									"is_bot": false,
								},
							},
						},
					},
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Fatalf("encode getUpdates response: %v", err)
				}
				return
			}

			if err := json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []any{}}); err != nil {
				t.Fatalf("encode empty getUpdates response: %v", err)
			}
		case "/bottest-token/sendMessage":
			var payload struct {
				ChatID int64  `json:"chat_id"`
				Text   string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode sendMessage payload: %v", err)
			}
			if payload.ChatID != 99 {
				t.Fatalf("unexpected chat id: %d", payload.ChatID)
			}
			mu.Lock()
			sentTexts = append(sentTexts, payload.Text)
			mu.Unlock()

			if err := json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"message_id": 30,
					"text":       payload.Text,
					"chat": map[string]any{
						"id":   payload.ChatID,
						"type": "private",
					},
				},
			}); err != nil {
				t.Fatalf("encode sendMessage response: %v", err)
			}

			cancel()
		case "/bottest-token/sendChatAction":
			var payload struct {
				ChatID int64  `json:"chat_id"`
				Action string `json:"action"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode sendChatAction payload: %v", err)
			}
			if payload.ChatID != 99 {
				t.Fatalf("unexpected typing chat id: %d", payload.ChatID)
			}
			if payload.Action != "typing" {
				t.Fatalf("unexpected typing action: %q", payload.Action)
			}
			mu.Lock()
			typingCalls++
			mu.Unlock()

			if err := json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true}); err != nil {
				t.Fatalf("encode sendChatAction response: %v", err)
			}
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	telegramClient, err := channels.NewTelegramWithBaseURL("test-token", server.URL, server.Client())
	if err != nil {
		t.Fatalf("create telegram client: %v", err)
	}

	err = RunTelegram(ctx, telegramClient, provider, 2*time.Second, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	provider.mu.Lock()
	if len(provider.inputs) != 1 {
		t.Fatalf("expected one provider request, got %d", len(provider.inputs))
	}
	if provider.inputs[0] != "Who are you?" {
		t.Fatalf("unexpected provider input: %q", provider.inputs[0])
	}
	provider.mu.Unlock()

	mu.Lock()
	if len(sentTexts) != 1 {
		t.Fatalf("expected one reply message, got %d", len(sentTexts))
	}
	if sentTexts[0] != "Hello from bot" {
		t.Fatalf("unexpected telegram reply: %q", sentTexts[0])
	}
	if typingCalls == 0 {
		t.Fatal("expected at least one typing action")
	}
	mu.Unlock()
}

func TestSplitTelegramMessage(t *testing.T) {
	text := strings.Repeat("a", telegramMaxMessageLength+13)
	chunks := splitTelegramMessage(text, telegramMaxMessageLength)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if len([]rune(chunks[0])) != telegramMaxMessageLength {
		t.Fatalf("unexpected first chunk length: %d", len([]rune(chunks[0])))
	}
	if got := chunks[0] + chunks[1]; got != text {
		t.Fatalf("chunk join mismatch")
	}
}

func TestRunTelegramPromptEmptyResponseFallback(t *testing.T) {
	provider := &telegramProviderStub{outputs: []providers.Msg{
		{Type: providers.MsgTypeToolCall, Value: `{"tool":"clock"}`},
		{Type: providers.MsgTypeToolResult, Value: "done"},
	}}

	response, err := runTelegramPrompt(context.Background(), provider, "Hello", time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if response != "(empty response)" {
		t.Fatalf("unexpected response: %q", response)
	}
}

func TestTelegramSenderLabel(t *testing.T) {
	tests := []struct {
		name    string
		message *channels.TelegramMessage
		want    string
	}{
		{
			name:    "user username",
			message: &channels.TelegramMessage{From: &channels.TelegramUser{Username: "alice"}},
			want:    "@alice",
		},
		{
			name:    "user full name",
			message: &channels.TelegramMessage{From: &channels.TelegramUser{FirstName: "Alice", LastName: "Doe"}},
			want:    "Alice Doe",
		},
		{
			name:    "user id fallback",
			message: &channels.TelegramMessage{From: &channels.TelegramUser{ID: 77}},
			want:    "user:77",
		},
		{
			name:    "sender chat title",
			message: &channels.TelegramMessage{SenderChat: &channels.TelegramChat{Title: "Announcements"}},
			want:    "Announcements",
		},
		{
			name:    "author signature",
			message: &channels.TelegramMessage{AuthorSignature: "Editor"},
			want:    "Editor",
		},
		{
			name:    "unknown",
			message: &channels.TelegramMessage{},
			want:    "unknown sender",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := telegramSenderLabel(tc.message)
			if got != tc.want {
				t.Fatalf("telegramSenderLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTelegramSenderID(t *testing.T) {
	tests := []struct {
		name    string
		message *channels.TelegramMessage
		want    string
	}{
		{
			name:    "with user id",
			message: &channels.TelegramMessage{From: &channels.TelegramUser{ID: 77}},
			want:    "77",
		},
		{
			name:    "no sender",
			message: &channels.TelegramMessage{},
			want:    "",
		},
		{
			name:    "nil message",
			message: nil,
			want:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := telegramSenderID(tc.message)
			if got != tc.want {
				t.Fatalf("telegramSenderID() = %q, want %q", got, tc.want)
			}
		})
	}
}
