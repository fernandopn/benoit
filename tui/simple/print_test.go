package simple

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestWriteHeader(t *testing.T) {
	t.Run("without width", func(t *testing.T) {
		var buf bytes.Buffer
		writer := bufio.NewWriter(&buf)

		WriteHeader(writer, NewTheme(false), "title", "hint", 0)
		if err := writer.Flush(); err != nil {
			t.Fatalf("flush: %v", err)
		}

		lines := strings.Split(buf.String(), "\n")
		if len(lines) != 3 {
			t.Fatalf("expected two printed lines plus terminator, got %d", len(lines))
		}
		if lines[0] != "title" {
			t.Fatalf("expected title on first line, got %q", lines[0])
		}
		if lines[1] != "" {
			t.Fatalf("expected trailing blank line, got %q", lines[1])
		}
		if strings.Contains(buf.String(), "hint") {
			t.Fatal("did not expect hint when width<=0")
		}
	})

	t.Run("with width", func(t *testing.T) {
		var buf bytes.Buffer
		writer := bufio.NewWriter(&buf)

		WriteHeader(writer, NewTheme(false), "title", "  hint with spaces  ", 100)
		if err := writer.Flush(); err != nil {
			t.Fatalf("flush: %v", err)
		}

		out := buf.String()
		if !strings.Contains(out, "title") {
			t.Fatal("expected title in output")
		}
		if !strings.Contains(out, "hint with spaces") {
			t.Fatal("expected trimmed hint in output")
		}
		if !strings.Contains(out, "\n\n") {
			t.Fatal("expected final blank line")
		}
	})
}

func TestWriteToolCardDefaultsAndCardStructure(t *testing.T) {
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	WriteToolCard(writer, NewTheme(false), 40, "", "a  b   c", "")
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.HasPrefix(out, "+") || !strings.HasSuffix(strings.TrimRight(out, "\n"), "+") {
		t.Fatalf("expected bordered card, got %q", out)
	}
	if !strings.Contains(out, "tool (a b c)") {
		t.Fatalf("expected tool name and compacted args, got %q", out)
	}
	if !strings.Contains(out, "(empty output)") {
		t.Fatalf("expected default empty output body, got %q", out)
	}
}

func TestCompactWhitespaceSimple(t *testing.T) {
	if got := compactWhitespace("  a  \t b\n c  "); got != "a b c" {
		t.Fatalf("unexpected compact result: %q", got)
	}
}

func TestWrapToWidthAndPadLine(t *testing.T) {
	if got := wrapToWidth("hello", 0); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("unexpected width 0 wrapping: %#v", got)
	}

	if got := wrapToWidth("", 4); len(got) != 1 || got[0] != "" {
		t.Fatalf("unexpected empty wrap: %#v", got)
	}

	got := wrapToWidth("abcdefghijk", 4)
	if len(got) != 3 || got[0] != "abcd" || got[1] != "efgh" || got[2] != "ijk" {
		t.Fatalf("unexpected wrapped lines: %#v", got)
	}

	if got := padLine("abc", 5); got != "abc  " {
		t.Fatalf("expected padded line, got %q", got)
	}
	if got := padLine("longer", 4); got != "long" {
		t.Fatalf("expected trimmed line, got %q", got)
	}
}
