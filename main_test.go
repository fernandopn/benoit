package main

import (
	"strings"
	"testing"

	"github.com/fernandopn/benoid/tools"
)

func TestSelectedTools(t *testing.T) {
	t.Run("all tools", func(t *testing.T) {
		selected, err := selectedTools(t.TempDir())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		names := toolNames(selected)
		if len(names) != 8 {
			t.Fatalf("expected 8 tools, got %d: %v", len(names), names)
		}
		expected := []string{"get_time", "code_interpreter", "web_search", "list_files", "get_current_directory", "maton_gcalendar", "maton_gmail", "read_file"}
		for i, want := range expected {
			if names[i] != want {
				t.Fatalf("tool order mismatch at %d: got %q expected %q", i, names[i], want)
			}
		}
	})

	t.Run("filesystem tools require root", func(t *testing.T) {
		_, err := selectedTools("")
		if err == nil {
			t.Fatal("expected restricted filesystem error")
		}
		if !strings.Contains(err.Error(), "root cannot be empty") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestParseTUIMode(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    bool
		wantErr bool
	}{
		{name: "simple", raw: "simple", want: true},
		{name: "bubbletea", raw: "bubbletea", want: false},
		{name: "trimmed", raw: " bubbletea ", want: false},
		{name: "case insensitive", raw: "SiMpLe", want: true},
		{name: "invalid", raw: "nope", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTUIMode(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseTUIMode(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func toolNames(tools []tools.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name())
	}
	return names
}
