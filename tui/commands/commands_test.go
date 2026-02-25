package commands

import "testing"

func TestKnownSuggestions(t *testing.T) {
	suggestions := KnownSuggestions()
	if len(suggestions) != 3 {
		t.Fatalf("expected 3 known suggestions, got %d", len(suggestions))
	}
	if suggestions[0].Command != CompactCommand {
		t.Fatalf("expected first suggestion %q, got %q", CompactCommand, suggestions[0].Command)
	}

	copy := KnownSuggestions()
	copy[0].Command = "/mutated"
	again := KnownSuggestions()
	if again[0].Command != CompactCommand {
		t.Fatalf("expected KnownSuggestions to return a copy, got %q", again[0].Command)
	}
}

func TestSuggestionsForPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		want   []string
	}{
		{name: "all from slash", prefix: "/", want: []string{CompactCommand, ExitCommand, QuitCommand}},
		{name: "leading spaces", prefix: " /", want: []string{CompactCommand, ExitCommand, QuitCommand}},
		{name: "trailing spaces", prefix: " /c  ", want: []string{CompactCommand}},
		{name: "tab prefix", prefix: "\t/e", want: []string{ExitCommand}},
		{name: "compact only", prefix: "/c", want: []string{CompactCommand}},
		{name: "exit only", prefix: "/e", want: []string{ExitCommand}},
		{name: "case-insensitive", prefix: "/C", want: []string{CompactCommand}},
		{name: "compact whitespace", prefix: " /Compact ", want: []string{CompactCommand}},
		{name: "no match", prefix: "/xyz", want: nil},
		{name: "invalid prefix", prefix: "compact", want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			suggestions := SuggestionsForPrefix(tc.prefix)
			if len(suggestions) != len(tc.want) {
				t.Fatalf("SuggestionsForPrefix(%q) len = %d, want %d", tc.prefix, len(suggestions), len(tc.want))
			}
			for i := range suggestions {
				if suggestions[i].Command != tc.want[i] {
					t.Fatalf("SuggestionsForPrefix(%q)[%d] = %q, want %q", tc.prefix, i, suggestions[i].Command, tc.want[i])
				}
			}
		})
	}
}

func TestSplitSlashCommandInput(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantCmd    string
		wantSuffix string
		wantOK     bool
	}{
		{name: "bare slash command", input: "/compact", wantCmd: "/compact", wantSuffix: "", wantOK: true},
		{name: "command with args", input: "/compact 100", wantCmd: "/compact", wantSuffix: " 100", wantOK: true},
		{name: "command with trailing spaces", input: "/compact   ", wantCmd: "/compact", wantSuffix: "   ", wantOK: true},
		{name: "command with tab args", input: "/compact\t100", wantCmd: "/compact", wantSuffix: "\t100", wantOK: true},
		{name: "non slash input", input: "hello", wantCmd: "", wantSuffix: "", wantOK: false},
		{name: "leading space", input: " /compact", wantCmd: "", wantSuffix: "", wantOK: false},
		{name: "multiline slash input", input: "/compact\nnext", wantCmd: "", wantSuffix: "", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotCmd, gotSuffix, gotOK := SplitSlashCommandInput(tc.input)
			if gotOK != tc.wantOK {
				t.Fatalf("SplitSlashCommandInput(%q) ok = %v, want %v", tc.input, gotOK, tc.wantOK)
			}
			if gotCmd != tc.wantCmd {
				t.Fatalf("SplitSlashCommandInput(%q) command = %q, want %q", tc.input, gotCmd, tc.wantCmd)
			}
			if gotSuffix != tc.wantSuffix {
				t.Fatalf("SplitSlashCommandInput(%q) suffix = %q, want %q", tc.input, gotSuffix, tc.wantSuffix)
			}
		})
	}
}

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
