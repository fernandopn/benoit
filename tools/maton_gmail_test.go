package tools

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestMatonGmailToolRequiresAPIKey(t *testing.T) {
	t.Setenv(MatonAPIKeyEnv, "")
	tool := NewMatonGmailToolWithHTTPClient(&recordingDoer{})
	out, err := tool.Call(context.Background(), map[string]any{"action": "list_messages"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "MATON_API_KEY is not set") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestMatonGmailToolListMessages(t *testing.T) {
	t.Setenv(MatonAPIKeyEnv, "test-key")
	doer := &recordingDoer{response: `{"messages":[{"id":"msg_1"}]}`}
	tool := NewMatonGmailToolWithHTTPClient(doer)

	out, err := tool.Call(context.Background(), map[string]any{
		"action":        "list_messages",
		"connection_id": "conn-123",
		"query": map[string]any{
			"maxResults": float64(10),
			"q":          "is:unread",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doer.req == nil {
		t.Fatal("expected request to be sent")
	}
	if doer.req.Method != http.MethodGet {
		t.Fatalf("expected GET method, got %s", doer.req.Method)
	}
	if !strings.Contains(doer.req.URL.Path, "/google-mail/gmail/v1/users/me/messages") {
		t.Fatalf("unexpected request path: %s", doer.req.URL.Path)
	}
	if got := doer.req.Header.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("unexpected authorization header: %q", got)
	}
	if got := doer.req.Header.Get("Maton-Connection"); got != "conn-123" {
		t.Fatalf("unexpected connection header: %q", got)
	}
	if got := doer.req.URL.Query().Get("q"); got != "is:unread" {
		t.Fatalf("unexpected q query value: %q", got)
	}
	if got := doer.req.URL.Query().Get("maxResults"); got != "10" {
		t.Fatalf("unexpected maxResults query value: %q", got)
	}
	if !strings.Contains(out, `"messages"`) {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestMatonGmailToolSendMessage(t *testing.T) {
	t.Setenv(MatonAPIKeyEnv, "test-key")
	doer := &recordingDoer{response: `{"id":"msg_1"}`}
	tool := NewMatonGmailToolWithHTTPClient(doer)

	out, err := tool.Call(context.Background(), map[string]any{
		"action": "send_message",
		"message": map[string]any{
			"raw": "BASE64_ENCODED_EMAIL",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doer.req == nil {
		t.Fatal("expected request to be sent")
	}
	if doer.req.Method != http.MethodPost {
		t.Fatalf("expected POST method, got %s", doer.req.Method)
	}
	if !strings.Contains(doer.req.URL.Path, "/google-mail/gmail/v1/users/me/messages/send") {
		t.Fatalf("unexpected request path: %s", doer.req.URL.Path)
	}
	if contentType := doer.req.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("expected JSON content type, got %q", contentType)
	}
	if !strings.Contains(doer.body, `"raw":"BASE64_ENCODED_EMAIL"`) {
		t.Fatalf("unexpected request body: %q", doer.body)
	}
	if !strings.Contains(out, `"msg_1"`) {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestMatonGmailToolSendDraftFromID(t *testing.T) {
	t.Setenv(MatonAPIKeyEnv, "test-key")
	doer := &recordingDoer{response: `{"id":"draft_1"}`}
	tool := NewMatonGmailToolWithHTTPClient(doer)

	_, err := tool.Call(context.Background(), map[string]any{
		"action":   "send_draft",
		"draft_id": "draft_123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(doer.req.URL.Path, "/google-mail/gmail/v1/users/me/drafts/send") {
		t.Fatalf("unexpected request path: %s", doer.req.URL.Path)
	}
	if !strings.Contains(doer.body, `"id":"draft_123"`) {
		t.Fatalf("unexpected request body: %q", doer.body)
	}
}

func TestMatonGmailToolInvalidAction(t *testing.T) {
	tool := NewMatonGmailToolWithClient(&MatonClient{})
	out, err := tool.Call(context.Background(), map[string]any{"action": "bogus"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: unsupported gmail action: bogus" {
		t.Fatalf("unexpected output: %q", out)
	}
}
