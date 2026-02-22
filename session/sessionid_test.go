package session

import (
	"strings"
	"testing"
)

func TestNormalizeSessionID(t *testing.T) {
	if got := NormalizeSessionID(" "); got != DefaultSessionID {
		t.Fatalf("NormalizeSessionID(empty) = %q, want %q", got, DefaultSessionID)
	}
	if got := NormalizeSessionID(" session-1 "); got != "session-1" {
		t.Fatalf("NormalizeSessionID(trimmed) = %q", got)
	}
}

func TestValidateSessionID(t *testing.T) {
	if err := ValidateSessionID("session-1"); err != nil {
		t.Fatalf("ValidateSessionID(session-1) unexpected error: %v", err)
	}
	if err := ValidateSessionID(" "); err != nil {
		t.Fatalf("ValidateSessionID(empty) unexpected error: %v", err)
	}

	if err := ValidateSessionID("bad\nline"); err == nil {
		t.Fatal("expected control-character validation error")
	}

	tooLong := strings.Repeat("a", 257)
	if err := ValidateSessionID(tooLong); err == nil {
		t.Fatal("expected max-length validation error")
	}
}

func TestResolveInteractiveSessionID(t *testing.T) {
	got, err := ResolveInteractiveSessionID(" session-abc ")
	if err != nil {
		t.Fatalf("ResolveInteractiveSessionID(trimmed) unexpected error: %v", err)
	}
	if got != "session-abc" {
		t.Fatalf("ResolveInteractiveSessionID(trimmed) = %q", got)
	}

	generated, err := ResolveInteractiveSessionID(" ")
	if err != nil {
		t.Fatalf("ResolveInteractiveSessionID(empty) unexpected error: %v", err)
	}
	if generated == "" {
		t.Fatal("expected generated session ID")
	}

	if _, err := ResolveInteractiveSessionID("bad\nline"); err == nil {
		t.Fatal("expected validation error for control character")
	}
}

func TestResolveTUISessionID(t *testing.T) {
	if got := ResolveTUISessionID(" session-123 "); got != "session-123" {
		t.Fatalf("ResolveTUISessionID(trimmed) = %q", got)
	}

	generated := ResolveTUISessionID(" ")
	if strings.TrimSpace(generated) == "" {
		t.Fatal("expected generated session ID")
	}

	fallback := ResolveTUISessionID("bad\nline")
	if strings.TrimSpace(fallback) == "" || strings.Contains(fallback, "\n") {
		t.Fatalf("expected fallback session ID, got %q", fallback)
	}
}

func TestTelegramSessionID(t *testing.T) {
	if got := TelegramSessionID(77); got != "telegram:77" {
		t.Fatalf("TelegramSessionID(77) = %q", got)
	}
	if got := TelegramSessionID(0); got != "" {
		t.Fatalf("TelegramSessionID(0) = %q", got)
	}
}
