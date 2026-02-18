package main

import (
	"strings"
	"testing"

	"github.com/fernandopn/benoid/tools"
)

func TestSelectedTools(t *testing.T) {
	t.Run("no tools disabled", func(t *testing.T) {
		selected, err := selectedTools(true, "", "")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if selected != nil {
			t.Fatalf("expected nil tool list, got %#v", selected)
		}
	})

	t.Run("all tools", func(t *testing.T) {
		selected, err := selectedTools(false, "", t.TempDir())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		names := toolNames(selected)
		if len(names) != 4 {
			t.Fatalf("expected 4 tools, got %d: %v", len(names), names)
		}
		expected := []string{"get_time", "get_current_directory", "list_files", "read_file"}
		for i, want := range expected {
			if names[i] != want {
				t.Fatalf("tool order mismatch at %d: got %q expected %q", i, names[i], want)
			}
		}
	})

	t.Run("subset tools", func(t *testing.T) {
		selected, err := selectedTools(false, "list_files, read_file", t.TempDir())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		names := toolNames(selected)
		expected := []string{"list_files", "read_file"}
		if len(names) != len(expected) {
			t.Fatalf("expected %d tools, got %d: %v", len(expected), len(names), names)
		}
		for i, want := range expected {
			if names[i] != want {
				t.Fatalf("tool order mismatch at %d: got %q expected %q", i, names[i], want)
			}
		}
	})

	t.Run("whitespace and duplicates", func(t *testing.T) {
		selected, err := selectedTools(false, " read_file , list_files, list_files ,", t.TempDir())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		names := toolNames(selected)
		expected := []string{"list_files", "read_file"}
		if len(names) != len(expected) {
			t.Fatalf("expected %d tools, got %d: %v", len(expected), len(names), names)
		}
		for i, want := range expected {
			if names[i] != want {
				t.Fatalf("tool order mismatch at %d: got %q expected %q", i, names[i], want)
			}
		}
	})

	t.Run("clock-only", func(t *testing.T) {
		selected, err := selectedTools(false, "clock", "")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got := len(selected); got != 1 {
			t.Fatalf("expected 1 tool, got %d", got)
		}
		if got := selected[0].Name(); got != "get_time" {
			t.Fatalf("expected get_time, got %q", got)
		}
	})

	t.Run("unknown tool", func(t *testing.T) {
		_, err := selectedTools(false, "list_files,bogus", t.TempDir())
		if err == nil {
			t.Fatal("expected unknown tool error")
		}
		if !strings.Contains(err.Error(), "unknown tool: bogus") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty tool list yields none", func(t *testing.T) {
		selected, err := selectedTools(false, "   , ,", t.TempDir())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if selected != nil {
			t.Fatalf("expected nil tool list, got %#v", selected)
		}
	})

	t.Run("filesystem tools require root", func(t *testing.T) {
		_, err := selectedTools(false, "list_files", "")
		if err == nil {
			t.Fatal("expected restricted filesystem error")
		}
		if !strings.Contains(err.Error(), "root cannot be empty") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func toolNames(tools []tools.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name())
	}
	return names
}
