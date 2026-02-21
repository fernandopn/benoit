package bubbletea

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestIsMouseEscapeKey(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyMsg
		want bool
	}{
		{
			name: "sgr mouse with esc prefix",
			msg:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1b[<64;20;9M")},
			want: true,
		},
		{
			name: "sgr mouse without esc prefix",
			msg:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<65;20;9m")},
			want: true,
		},
		{
			name: "normal typing",
			msg:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")},
			want: false,
		},
		{
			name: "page up key",
			msg:  tea.KeyMsg{Type: tea.KeyPgUp},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMouseEscapeKey(tc.msg); got != tc.want {
				t.Fatalf("unexpected result: got=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestUpdateIgnoresMouseEscapeRunesInInput(t *testing.T) {
	m := newSizedTestModel(t)
	m.input.SetValue("hello")

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1b[<64;20;9M")})

	if got := m.input.Value(); got != "hello" {
		t.Fatalf("expected mouse escape sequence to be ignored, got %q", got)
	}
}

func TestUpdateMouseWheelDoesNotMutateInput(t *testing.T) {
	m := newSizedTestModel(t)
	m.input.SetValue("hello")

	m = applyUpdate(t, m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})

	if got := m.input.Value(); got != "hello" {
		t.Fatalf("expected mouse wheel event to not change input, got %q", got)
	}
}
