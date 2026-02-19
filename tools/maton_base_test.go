package tools

import (
	"strings"
	"testing"
)

func TestNewMatonClientRequiresKey(t *testing.T) {
	_, err := NewMatonClient("", nil)
	if err == nil {
		t.Fatal("expected error when api key is missing")
	}
	if !strings.Contains(err.Error(), "api key cannot be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildMatonURL(t *testing.T) {
	url, err := buildMatonURL("https://gateway.maton.ai", "/google-calendar/calendar/v3/freeBusy", map[string]string{
		"singleEvents": "true",
		"maxResults":   "10",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(url, "https://gateway.maton.ai/google-calendar/calendar/v3/freeBusy?") {
		t.Fatalf("unexpected url prefix: %q", url)
	}
	if !strings.Contains(url, "singleEvents=true") {
		t.Fatalf("expected singleEvents query param in %q", url)
	}
	if !strings.Contains(url, "maxResults=10") {
		t.Fatalf("expected maxResults query param in %q", url)
	}
}

func TestObjectToQuery(t *testing.T) {
	query, err := objectToQuery(map[string]any{
		"s": "abc",
		"b": true,
		"n": float64(12),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query["s"] != "abc" {
		t.Fatalf("unexpected string conversion: %q", query["s"])
	}
	if query["b"] != "true" {
		t.Fatalf("unexpected bool conversion: %q", query["b"])
	}
	if query["n"] != "12" {
		t.Fatalf("unexpected number conversion: %q", query["n"])
	}
}
