package files

import "testing"

func TestNewToolSetRequiresRoot(t *testing.T) {
	if _, err := NewToolSet(""); err == nil {
		t.Fatal("expected fs root validation error")
	}
}

func TestNewToolSetIncludesExpectedTools(t *testing.T) {
	toolSet, err := NewToolSet("/tmp/benoit-tools")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toolSet) != 3 {
		t.Fatalf("unexpected tool count: %d", len(toolSet))
	}
	got := []string{toolSet[0].Name(), toolSet[1].Name(), toolSet[2].Name()}
	want := []string{"list_files", "get_current_directory", "read_file"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tool order mismatch at %d: got %q want %q", i, got[i], want[i])
		}
	}
}
