package commands

import "testing"

func TestParseCompact(t *testing.T) {
	parsed, err := Parse("/compact")
	if err != nil {
		t.Fatalf("Parse(/compact) unexpected error: %v", err)
	}
	if parsed.Kind != KindCompact || parsed.MaxWords != DefaultCompressionMaxWords {
		t.Fatalf("unexpected parse result: %#v", parsed)
	}

	parsed, err = Parse("/compact 77")
	if err != nil {
		t.Fatalf("Parse(/compact 77) unexpected error: %v", err)
	}
	if parsed.Kind != KindCompact || parsed.MaxWords != 77 {
		t.Fatalf("unexpected parse result: %#v", parsed)
	}

	parsed, err = Parse("/compact nope")
	if err == nil {
		t.Fatal("expected parse error for invalid /compact argument")
	}
	if parsed.Kind != KindCompact {
		t.Fatalf("expected KindCompact on usage error, got %#v", parsed)
	}
}

func TestParseExit(t *testing.T) {
	parsed, err := Parse(" /exit ")
	if err != nil {
		t.Fatalf("Parse(/exit) unexpected error: %v", err)
	}
	if parsed.Kind != KindExit {
		t.Fatalf("expected exit command, got %#v", parsed)
	}

	parsed, err = Parse("/quit")
	if err != nil {
		t.Fatalf("Parse(/quit) unexpected error: %v", err)
	}
	if parsed.Kind != KindExit {
		t.Fatalf("expected exit command, got %#v", parsed)
	}

	parsed, err = Parse("/quit now")
	if err != nil {
		t.Fatalf("Parse(/quit now) unexpected error: %v", err)
	}
	if parsed.Kind != KindNone {
		t.Fatalf("expected plain prompt when /quit has extra tokens, got %#v", parsed)
	}
}

func TestParseCompactHelper(t *testing.T) {
	maxWords, ok, err := ParseCompact("hello")
	if err != nil || ok || maxWords != 0 {
		t.Fatalf("unexpected ParseCompact(non-command) result: max=%d ok=%v err=%v", maxWords, ok, err)
	}

	maxWords, ok, err = ParseCompact("/compact 123")
	if err != nil || !ok || maxWords != 123 {
		t.Fatalf("unexpected ParseCompact result: max=%d ok=%v err=%v", maxWords, ok, err)
	}
}

func TestIsExit(t *testing.T) {
	if !IsExit(" /exit ") {
		t.Fatal("expected /exit to be recognized")
	}
	if !IsExit("/quit") {
		t.Fatal("expected /quit to be recognized")
	}
	if IsExit("/quit now") {
		t.Fatal("did not expect /quit with args to be treated as exit")
	}
}
