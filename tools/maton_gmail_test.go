package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestMatonGmailToolRequiresAPIKey(t *testing.T) {
	tool := NewMatonGmailTool(nil)
	out, err := tool.Call(context.Background(), map[string]any{"action": "list_messages"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "maton client is not configured") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestMatonGmailToolListMessages(t *testing.T) {
	doer := &recordingDoer{response: `{"messages":[{"id":"msg_1"}]}`}
	tool := NewMatonGmailTool(newMatonToolClient(t, doer))

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
	doer := &recordingDoer{response: `{"id":"msg_1"}`}
	tool := NewMatonGmailTool(newMatonToolClient(t, doer))
	rfc822 := "To: receiver@example.com\r\nSubject: Hello\r\n\r\nHi"
	raw := base64.RawURLEncoding.EncodeToString([]byte(rfc822))

	out, err := tool.Call(context.Background(), map[string]any{
		"action": "send_message",
		"message": map[string]any{
			"raw": raw,
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
	decoded := extractRawFromBody(t, doer.body)
	if decoded != rfc822 {
		t.Fatalf("unexpected raw payload:\nwant: %q\n got: %q", rfc822, decoded)
	}
	if !strings.Contains(out, `"msg_1"`) {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestMatonGmailToolSendMessageFromStructuredFields(t *testing.T) {
	doer := &recordingDoer{response: `{"id":"msg_2"}`}
	tool := NewMatonGmailTool(newMatonToolClient(t, doer))

	_, err := tool.Call(context.Background(), map[string]any{
		"action": "send_message",
		"message": map[string]any{
			"to":      "receiver@example.com",
			"subject": "Hello",
			"body":    "Line 1\nLine 2",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw := extractRawFromBody(t, doer.body)
	if !strings.Contains(raw, "To: receiver@example.com\r\n") {
		t.Fatalf("expected To header in raw payload, got %q", raw)
	}
	if !strings.Contains(raw, "Subject: Hello\r\n") {
		t.Fatalf("expected Subject header in raw payload, got %q", raw)
	}
	if !strings.Contains(raw, "\r\n\r\nLine 1\r\nLine 2") {
		t.Fatalf("expected CRLF-normalized body in raw payload, got %q", raw)
	}
}

func TestMatonGmailToolSendMessageFromTopLevelStructuredFields(t *testing.T) {
	doer := &recordingDoer{response: `{"id":"msg_3"}`}
	tool := NewMatonGmailTool(newMatonToolClient(t, doer))

	_, err := tool.Call(context.Background(), map[string]any{
		"action":  "send_message",
		"to":      "receiver@example.com",
		"subject": "Top Level",
		"body":    "Hello from top level",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw := extractRawFromBody(t, doer.body)
	if !strings.Contains(raw, "To: receiver@example.com\r\n") {
		t.Fatalf("expected To header in raw payload, got %q", raw)
	}
	if !strings.Contains(raw, "Subject: Top Level\r\n") {
		t.Fatalf("expected Subject header in raw payload, got %q", raw)
	}
	if !strings.Contains(raw, "\r\n\r\nHello from top level") {
		t.Fatalf("expected body in raw payload, got %q", raw)
	}
}

func TestMatonGmailToolSendMessageRejectsInvalidRaw(t *testing.T) {
	tool := NewMatonGmailTool(newMatonToolClient(t, &recordingDoer{}))

	out, err := tool.Call(context.Background(), map[string]any{
		"action": "send_message",
		"message": map[string]any{
			"raw": "%%%invalid%%%",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "message.raw must be base64url-encoded RFC822") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestMatonGmailToolSendDraftFromID(t *testing.T) {
	doer := &recordingDoer{response: `{"id":"draft_1"}`}
	tool := NewMatonGmailTool(newMatonToolClient(t, doer))

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
	tool := NewMatonGmailTool(&MatonClient{})
	out, err := tool.Call(context.Background(), map[string]any{"action": "bogus"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: unsupported gmail action: bogus" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func extractRawFromBody(t *testing.T, body string) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("failed to decode request body: %v", err)
	}
	rawValue, ok := payload["raw"]
	if !ok {
		t.Fatalf("request body missing raw field: %q", body)
	}
	rawString, ok := rawValue.(string)
	if !ok {
		t.Fatalf("raw field is not a string: %#v", rawValue)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(rawString)
	if err != nil {
		t.Fatalf("raw is not valid base64url: %v", err)
	}
	return string(decoded)
}

func newMatonToolClient(t *testing.T, doer httpDoer) *MatonClient {
	t.Helper()
	client, err := NewMatonClient("test-key", doer)
	if err != nil {
		t.Fatalf("new maton client: %v", err)
	}
	return client
}
