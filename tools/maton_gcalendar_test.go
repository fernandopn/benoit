package tools

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordingDoer struct {
	req      *http.Request
	body     string
	status   int
	response string
	err      error
}

func (r *recordingDoer) Do(req *http.Request) (*http.Response, error) {
	r.req = req
	if req.Body != nil {
		payload, _ := io.ReadAll(req.Body)
		r.body = string(payload)
	}
	if r.err != nil {
		return nil, r.err
	}
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(r.response)),
		Header:     make(http.Header),
	}, nil
}

func TestMatonGCalendarToolRequiresAPIKey(t *testing.T) {
	t.Setenv(MatonAPIKeyEnv, "")
	tool := NewMatonGCalendarToolWithHTTPClient(&recordingDoer{})
	out, err := tool.Call(context.Background(), map[string]any{"action": "list_calendars"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "MATON_API_KEY is not set") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestMatonGCalendarToolListEvents(t *testing.T) {
	t.Setenv(MatonAPIKeyEnv, "test-key")
	doer := &recordingDoer{response: `{"items":[{"id":"evt_1"}]}`}
	tool := NewMatonGCalendarToolWithHTTPClient(doer)

	out, err := tool.Call(context.Background(), map[string]any{
		"action":        "list_events",
		"calendar_id":   "primary",
		"connection_id": "conn-123",
		"query": map[string]any{
			"maxResults":   float64(10),
			"singleEvents": true,
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
	if !strings.Contains(doer.req.URL.Path, "/google-calendar/calendar/v3/calendars/primary/events") {
		t.Fatalf("unexpected request path: %s", doer.req.URL.Path)
	}
	if got := doer.req.Header.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("unexpected authorization header: %q", got)
	}
	if got := doer.req.Header.Get("Maton-Connection"); got != "conn-123" {
		t.Fatalf("unexpected connection header: %q", got)
	}
	if got := doer.req.URL.Query().Get("maxResults"); got != "10" {
		t.Fatalf("unexpected maxResults query value: %q", got)
	}
	if got := doer.req.URL.Query().Get("singleEvents"); got != "true" {
		t.Fatalf("unexpected singleEvents query value: %q", got)
	}
	if !strings.Contains(out, "\"items\"") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestMatonGCalendarToolCreateEvent(t *testing.T) {
	t.Setenv(MatonAPIKeyEnv, "test-key")
	doer := &recordingDoer{response: `{"id":"evt_1"}`}
	tool := NewMatonGCalendarToolWithHTTPClient(doer)

	out, err := tool.Call(context.Background(), map[string]any{
		"action":      "create_event",
		"calendar_id": "primary",
		"event": map[string]any{
			"summary": "Team Sync",
			"start":   map[string]any{"dateTime": "2026-02-18T10:00:00Z"},
			"end":     map[string]any{"dateTime": "2026-02-18T10:30:00Z"},
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
	if !strings.Contains(doer.req.URL.Path, "/google-calendar/calendar/v3/calendars/primary/events") {
		t.Fatalf("unexpected request path: %s", doer.req.URL.Path)
	}
	if contentType := doer.req.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("expected JSON content type, got %q", contentType)
	}
	if !strings.Contains(doer.body, "\"summary\":\"Team Sync\"") {
		t.Fatalf("unexpected request body: %q", doer.body)
	}
	if !strings.Contains(out, "\"evt_1\"") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestMatonGCalendarToolInvalidAction(t *testing.T) {
	tool := NewMatonGCalendarToolWithClient(&MatonClient{})
	out, err := tool.Call(context.Background(), map[string]any{"action": "bogus"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: unsupported action: bogus" {
		t.Fatalf("unexpected output: %q", out)
	}
}
