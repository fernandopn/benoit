package channels

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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

	message, err := client.SendMessage(context.Background(), 99, "hello")
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if !called {
		t.Fatal("expected sendMessage request to be called")
	}
	if message.ID != 123 {
		t.Fatalf("unexpected message ID: %d", message.ID)
	}
	if message.Chat.ID != 99 {
		t.Fatalf("unexpected chat ID in response: %d", message.Chat.ID)
	}
	if message.Text != "hello" {
		t.Fatalf("unexpected message text in response: %q", message.Text)
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

	_, err = client.SendMessage(context.Background(), 99, "hello")
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
