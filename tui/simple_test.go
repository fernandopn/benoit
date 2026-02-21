package tui

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestIsShiftEnterSequence(t *testing.T) {
	tests := []struct {
		name string
		seq  string
		want bool
	}{
		{name: "csi-u form", seq: "\x1b[13;2u", want: true},
		{name: "xterm form", seq: "\x1b[27;2;13~", want: true},
		{name: "plain enter", seq: "\x1b[13u", want: false},
		{name: "arrow key", seq: "\x1b[A", want: false},
		{name: "not csi", seq: "abc", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isShiftEnterSequence(tc.seq)
			if got != tc.want {
				t.Fatalf("isShiftEnterSequence(%q) = %v, want %v", tc.seq, got, tc.want)
			}
		})
	}
}

func TestDecodeRuneFromFirstByte(t *testing.T) {
	reader := bufio.NewReader(bytes.NewBuffer([]byte{0xa9}))
	r, raw, err := decodeRuneFromFirstByte(reader, 0xc3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r != 'é' {
		t.Fatalf("unexpected rune: %q", r)
	}
	if len(raw) != 2 || raw[0] != 0xc3 || raw[1] != 0xa9 {
		t.Fatalf("unexpected raw bytes: %v", raw)
	}
}

func TestResolveTUISessionID(t *testing.T) {
	if got := resolveTUISessionID(" session-123 "); got != "session-123" {
		t.Fatalf("resolveTUISessionID(trimmed) = %q", got)
	}

	generated := resolveTUISessionID(" ")
	if strings.TrimSpace(generated) == "" {
		t.Fatal("expected generated session ID")
	}

	fallback := resolveTUISessionID("bad\nline")
	if strings.TrimSpace(fallback) == "" || strings.Contains(fallback, "\n") {
		t.Fatalf("expected fallback session ID, got %q", fallback)
	}
}
