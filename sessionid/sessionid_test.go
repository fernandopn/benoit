package sessionid

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	if got := Normalize(" "); got != Default {
		t.Fatalf("Normalize(empty) = %q, want %q", got, Default)
	}
	if got := Normalize(" session-1 "); got != "session-1" {
		t.Fatalf("Normalize(trimmed) = %q", got)
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("session-1"); err != nil {
		t.Fatalf("Validate(session-1) unexpected error: %v", err)
	}
	if err := Validate(" "); err != nil {
		t.Fatalf("Validate(empty) unexpected error: %v", err)
	}

	if err := Validate("bad\nline"); err == nil {
		t.Fatal("expected control-character validation error")
	}

	tooLong := strings.Repeat("a", 257)
	if err := Validate(tooLong); err == nil {
		t.Fatal("expected max-length validation error")
	}
}

func TestResolveInteractive(t *testing.T) {
	got, err := ResolveInteractive(" session-abc ")
	if err != nil {
		t.Fatalf("ResolveInteractive(trimmed) unexpected error: %v", err)
	}
	if got != "session-abc" {
		t.Fatalf("ResolveInteractive(trimmed) = %q", got)
	}

	generated, err := ResolveInteractive(" ")
	if err != nil {
		t.Fatalf("ResolveInteractive(empty) unexpected error: %v", err)
	}
	if generated == "" {
		t.Fatal("expected generated session ID")
	}

	if _, err := ResolveInteractive("bad\nline"); err == nil {
		t.Fatal("expected validation error for control character")
	}
}

func TestTelegram(t *testing.T) {
	if got := Telegram(77); got != "telegram:77" {
		t.Fatalf("Telegram(77) = %q", got)
	}
	if got := Telegram(0); got != "" {
		t.Fatalf("Telegram(0) = %q", got)
	}
}
