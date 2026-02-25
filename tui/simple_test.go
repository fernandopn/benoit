package tui

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	tuicmd "github.com/fernandopn/benoit/tui/commands"
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

func TestCompleteSimpleSlashInput(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantOutput     string
		wantHandled    bool
		wantMatchCount int
	}{
		{name: "non command", input: "hello", wantOutput: "hello", wantHandled: false, wantMatchCount: 0},
		{name: "unique completion", input: "/comp", wantOutput: "/compact", wantHandled: true, wantMatchCount: 1},
		{name: "unique with suffix", input: "/comp 10", wantOutput: "/compact 10", wantHandled: true, wantMatchCount: 1},
		{name: "ambiguous", input: "/", wantOutput: "/", wantHandled: true, wantMatchCount: 3},
		{name: "no command match", input: "/zzz", wantOutput: "/zzz", wantHandled: true, wantMatchCount: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotOutput, gotMatches, gotHandled := completeSimpleSlashInput(tc.input)
			if gotHandled != tc.wantHandled {
				t.Fatalf("completeSimpleSlashInput(%q) handled = %v, want %v", tc.input, gotHandled, tc.wantHandled)
			}
			if gotOutput != tc.wantOutput {
				t.Fatalf("completeSimpleSlashInput(%q) output = %q, want %q", tc.input, gotOutput, tc.wantOutput)
			}
			if len(gotMatches) != tc.wantMatchCount {
				t.Fatalf("completeSimpleSlashInput(%q) matches = %d, want %d", tc.input, len(gotMatches), tc.wantMatchCount)
			}
		})
	}
}

func TestReplaceSimpleInput(t *testing.T) {
	tests := []struct {
		name    string
		current []rune
		next    string
		want    string
	}{
		{
			name:    "ascii replacement",
			current: []rune("/comp"),
			next:    "/compact",
			want:    "\b \b\b \b\b \b\b \b\b \b/compact",
		},
		{
			name:    "multibyte replacement",
			current: []rune("/é"),
			next:    "/exit",
			want:    "\b \b\b \b/exit",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			writer := bufio.NewWriter(&buf)

			if err := replaceSimpleInput(writer, tc.current, tc.next); err != nil {
				t.Fatalf("replaceSimpleInput() unexpected error: %v", err)
			}

			if got := buf.String(); got != tc.want {
				t.Fatalf("replaceSimpleInput() output = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrintSimpleCompletionSuggestions(t *testing.T) {
	t.Run("renders suggestion lines and prompt", func(t *testing.T) {
		suggestions := []tuicmd.Suggestion{
			{Command: "/compact", Description: "compact context"},
			{Command: "/exit", Description: "quit session"},
			{Command: "/quit", Description: "quit session"},
		}

		var buf bytes.Buffer
		writer := bufio.NewWriter(&buf)
		err := printSimpleCompletionSuggestions(writer, ">: ", "/", suggestions)
		if err != nil {
			t.Fatalf("printSimpleCompletionSuggestions() unexpected error: %v", err)
		}

		got := buf.String()
		parts := strings.Split(got, "\r\n")
		if len(parts) < 4 {
			t.Fatalf("expected suggestion output to include prompt and three lines, got %q", got)
		}
		if !strings.HasPrefix(got, "\r\n") {
			t.Fatalf("expected output to start with newline, got %q", got)
		}
		if !strings.Contains(got, "  /compact - compact context") {
			t.Fatalf("expected compact suggestion line, got %q", got)
		}
		if !strings.Contains(got, "  /exit - quit session") {
			t.Fatalf("expected exit suggestion line, got %q", got)
		}
		if !strings.HasSuffix(got, ">: /") {
			t.Fatalf("expected output to end with restored prompt+input, got %q", got)
		}
	})

	t.Run("no suggestions is noop", func(t *testing.T) {
		var buf bytes.Buffer
		writer := bufio.NewWriter(&buf)
		if err := printSimpleCompletionSuggestions(writer, ">: ", "/", nil); err != nil {
			t.Fatalf("printSimpleCompletionSuggestions() unexpected error: %v", err)
		}
		if got := buf.String(); got != "" {
			t.Fatalf("expected no output for empty suggestions, got %q", got)
		}
	})
}
