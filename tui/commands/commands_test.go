package commands

import "testing"

func TestParseCompress(t *testing.T) {
	parsed, err := Parse("/compress")
	if err != nil {
		t.Fatalf("Parse(/compress) unexpected error: %v", err)
	}
	if parsed.Kind != KindCompress || parsed.MaxWords != DefaultCompressionMaxWords {
		t.Fatalf("unexpected parse result: %#v", parsed)
	}

	parsed, err = Parse("/compress 77")
	if err != nil {
		t.Fatalf("Parse(/compress 77) unexpected error: %v", err)
	}
	if parsed.Kind != KindCompress || parsed.MaxWords != 77 {
		t.Fatalf("unexpected parse result: %#v", parsed)
	}

	parsed, err = Parse("/compress nope")
	if err == nil {
		t.Fatal("expected parse error for invalid /compress argument")
	}
	if parsed.Kind != KindCompress {
		t.Fatalf("expected KindCompress on usage error, got %#v", parsed)
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

func TestParseCompressHelper(t *testing.T) {
	maxWords, ok, err := ParseCompress("hello")
	if err != nil || ok || maxWords != 0 {
		t.Fatalf("unexpected ParseCompress(non-command) result: max=%d ok=%v err=%v", maxWords, ok, err)
	}

	maxWords, ok, err = ParseCompress("/compress 123")
	if err != nil || !ok || maxWords != 123 {
		t.Fatalf("unexpected ParseCompress result: max=%d ok=%v err=%v", maxWords, ok, err)
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
