package channels

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewTelegramValidation(t *testing.T) {
	_, err := NewTelegram("", &http.Client{})
	if err == nil {
		t.Fatal("expected missing token error")
	}

	_, err = NewTelegram("bot-token", nil)
	if err == nil {
		t.Fatal("expected missing http client error")
	}

	_, err = NewTelegramWithBaseURL("bot-token", "", &http.Client{})
	if err == nil {
		t.Fatal("expected missing base URL error")
	}

	client, err := NewTelegram("  env-token  ", &http.Client{})
	if err != nil {
		t.Fatalf("expected constructor to succeed: %v", err)
	}
	if client.botToken != "env-token" {
		t.Fatalf("unexpected trimmed bot token: %q", client.botToken)
	}
}

func TestSendMessage(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/botbot-token/sendMessage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		var payload struct {
			ChatID int64  `json:"chat_id"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.ChatID != 99 {
			t.Fatalf("unexpected chat id: %d", payload.ChatID)
		}
		if payload.Text != "hello" {
			t.Fatalf("unexpected message text: %q", payload.Text)
		}

		response := map[string]any{
			"ok": true,
			"result": map[string]any{
				"message_id": 123,
				"text":       "hello",
				"chat": map[string]any{
					"id":   99,
					"type": "private",
				},
			},
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client, err := NewTelegramWithBaseURL("bot-token", server.URL, server.Client())
	if err != nil {
		t.Fatalf("new telegram client: %v", err)
	}

	err = client.SendMessage(context.Background(), ChannelMessage{Text: "hello", UserID: 99, Type: TextMessage})
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if !called {
		t.Fatal("expected sendMessage request to be called")
	}
}

func TestReceiveMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botbot-token/getUpdates" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		var payload struct {
			Offset  int64 `json:"offset"`
			Timeout int   `json:"timeout"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.Offset != 55 {
			t.Fatalf("unexpected offset: %d", payload.Offset)
		}
		if payload.Timeout != 30 {
			t.Fatalf("unexpected timeout: %d", payload.Timeout)
		}

		response := map[string]any{
			"ok": true,
			"result": []map[string]any{
				{
					"update_id": 1001,
					"message": map[string]any{
						"message_id": 77,
						"text":       "ping",
						"chat": map[string]any{
							"id":   42,
							"type": "private",
						},
					},
				},
			},
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client, err := NewTelegramWithBaseURL("bot-token", server.URL, server.Client())
	if err != nil {
		t.Fatalf("new telegram client: %v", err)
	}

	updates, err := client.ReceiveMessages(context.Background(), 55, 30)
	if err != nil {
		t.Fatalf("receive messages: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("expected one update, got %d", len(updates))
	}
	if updates[0].ID != 1001 {
		t.Fatalf("unexpected update ID: %d", updates[0].ID)
	}
	if updates[0].Message == nil {
		t.Fatal("expected message in update")
	}
	if updates[0].Message.Text != "ping" {
		t.Fatalf("unexpected update text: %q", updates[0].Message.Text)
	}
}

func TestTelegramAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]any{
			"ok":          false,
			"error_code":  400,
			"description": "Bad Request: chat not found",
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client, err := NewTelegramWithBaseURL("bot-token", server.URL, server.Client())
	if err != nil {
		t.Fatalf("new telegram client: %v", err)
	}

	err = client.SendMessage(context.Background(), ChannelMessage{Text: "hello", UserID: 99, Type: TextMessage})
	if err == nil {
		t.Fatal("expected API error")
	}

	var apiErr *TelegramAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected TelegramAPIError, got %T", err)
	}
	if apiErr.ErrorCode != 400 {
		t.Fatalf("unexpected error code: %d", apiErr.ErrorCode)
	}
	if apiErr.Description != "Bad Request: chat not found" {
		t.Fatalf("unexpected description: %q", apiErr.Description)
	}
}

func TestSendTyping(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/botbot-token/sendChatAction" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		var payload struct {
			ChatID int64  `json:"chat_id"`
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.ChatID != 42 {
			t.Fatalf("unexpected chat id: %d", payload.ChatID)
		}
		if payload.Action != "typing" {
			t.Fatalf("unexpected action: %q", payload.Action)
		}

		if err := json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client, err := NewTelegramWithBaseURL("bot-token", server.URL, server.Client())
	if err != nil {
		t.Fatalf("new telegram client: %v", err)
	}

	if err := client.SendTyping(context.Background(), 42); err != nil {
		t.Fatalf("send typing: %v", err)
	}
	if !called {
		t.Fatal("expected sendChatAction to be called")
	}

	if err := client.SendTyping(context.Background(), 0); err == nil {
		t.Fatal("expected missing chat ID error")
	}
}

func TestListenBroadcastsToMultipleReceiveChannels(t *testing.T) {
	var updateCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botbot-token/getUpdates" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		updateCalls++
		if updateCalls == 1 {
			response := map[string]any{
				"ok": true,
				"result": []map[string]any{
					{
						"update_id": 1001,
						"message": map[string]any{
							"message_id": 77,
							"text":       "ping",
							"chat": map[string]any{
								"id":   42,
								"type": "private",
							},
							"from": map[string]any{
								"id":       77,
								"is_bot":   false,
								"username": "alice",
							},
						},
					},
				},
			}
			if err := json.NewEncoder(w).Encode(response); err != nil {
				t.Fatalf("encode response: %v", err)
			}
			return
		}

		if err := json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []any{}}); err != nil {
			t.Fatalf("encode empty response: %v", err)
		}
	}))
	defer server.Close()

	client, err := NewTelegramWithBaseURL("bot-token", server.URL, server.Client())
	if err != nil {
		t.Fatalf("new telegram client: %v", err)
	}

	first := make(chan ChannelMessage, 1)
	second := make(chan ChannelMessage, 1)
	if err := client.RegisterReceiveMessageChan(first); err != nil {
		t.Fatalf("register first channel: %v", err)
	}
	if err := client.RegisterReceiveMessageChan(second); err != nil {
		t.Fatalf("register second channel: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Listen(ctx, 0)
	}()

	waitForMessage := func(name string, receive <-chan ChannelMessage) ChannelMessage {
		t.Helper()
		select {
		case message := <-receive:
			return message
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s message", name)
			return ChannelMessage{}
		}
	}

	firstMessage := waitForMessage("first", first)
	secondMessage := waitForMessage("second", second)
	for _, message := range []ChannelMessage{firstMessage, secondMessage} {
		if message.Type != TextMessage {
			t.Fatalf("unexpected message type: %d", message.Type)
		}
		if message.Text != "ping" {
			t.Fatalf("unexpected message text: %q", message.Text)
		}
		if message.UserID != 77 {
			t.Fatalf("unexpected message user id: %d", message.UserID)
		}
		if message.Params[ParamUsername] != "alice" {
			t.Fatalf("unexpected username param: %q", message.Params[ParamUsername])
		}
		if message.Params[ParamDisplayName] != "@alice" {
			t.Fatalf("unexpected display_name param: %q", message.Params[ParamDisplayName])
		}
	}

	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for listen to stop")
	}
}

func TestToChannelMessageDisplayNameFallback(t *testing.T) {
	message := &TelegramMessage{
		Text: "hello",
		Chat: TelegramChat{ID: 77, Type: "private"},
		From: &TelegramUser{ID: 77, FirstName: "Fernando", LastName: "PN"},
	}

	channelMessage, ok := toChannelMessage(message)
	if !ok {
		t.Fatal("expected message to be converted")
	}
	if channelMessage.Params[ParamUsername] != "" {
		t.Fatalf("expected empty username, got %q", channelMessage.Params[ParamUsername])
	}
	if channelMessage.Params[ParamDisplayName] != "Fernando PN" {
		t.Fatalf("unexpected display_name param: %q", channelMessage.Params[ParamDisplayName])
	}
}
