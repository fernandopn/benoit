package bubbletea

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	tuicmd "github.com/fernandopn/benoit/tui/commands"
)

func applyUpdate(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	next, _ := m.Update(msg)
	updated, ok := next.(model)
	if !ok {
		t.Fatalf("expected model update to return bubbletea.model, got %T", next)
	}
	return updated
}

func applyUpdateWithCmd(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	updated, ok := next.(model)
	if !ok {
		t.Fatalf("expected model update to return bubbletea.model, got %T", next)
	}
	return updated, cmd
}

func newSizedTestModel(t *testing.T) model {
	t.Helper()
	m := newTestModel()
	return applyUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
}

func TestTabCompletesUniqueSlashCommand(t *testing.T) {
	m := newSizedTestModel(t)
	m.input.SetValue("/comp")

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})

	if got := m.input.Value(); got != "/compact" {
		t.Fatalf("expected tab completion to produce /compact, got %q", got)
	}
	if m.commandSuggestionsShown {
		t.Fatalf("expected suggestion table to stay hidden for unique completion")
	}
}

func TestTabShowsSlashCommandTableWhenAmbiguous(t *testing.T) {
	m := newSizedTestModel(t)
	m.input.SetValue("/")

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})

	if !m.commandSuggestionsShown {
		t.Fatalf("expected suggestion table to be visible")
	}
	if len(m.cmds.Rows()) != len(knownSlashCommands) {
		t.Fatalf("expected %d suggestions, got %d", len(knownSlashCommands), len(m.cmds.Rows()))
	}

	if got := m.cmds.Height(); got < commandSuggestionMinRows {
		t.Fatalf("expected suggestion table height >= %d rows, got %d", commandSuggestionMinRows, got)
	}

	selected, ok := m.selectedCommandSuggestion()
	if !ok {
		t.Fatalf("expected first command to be selected")
	}
	if selected != knownSlashCommands[0].Command {
		t.Fatalf("expected first selected command %q, got %q", knownSlashCommands[0].Command, selected)
	}

	suggestions := m.renderCommandSuggestions()
	if suggestions == "" {
		t.Fatalf("expected non-empty suggestion table rendering")
	}
	if !strings.Contains(ansi.Strip(suggestions), "/compact") {
		t.Fatalf("expected suggestion table to include /compact")
	}
	if !strings.Contains(ansi.Strip(m.renderInputArea()), "/") {
		t.Fatalf("expected input area to keep the typed slash command")
	}

	full := m.View()
	sIdx := strings.Index(full, suggestions)
	iIdx := strings.Index(full, m.renderInputArea())
	if sIdx < 0 || iIdx < 0 || sIdx >= iIdx {
		t.Fatalf("expected suggestions to render above input area")
	}
}

func TestArrowSelectionAndEnterSendsSelectedSuggestion(t *testing.T) {
	m := newSizedTestModel(t)
	m.input.SetValue("/")
	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})

	selected, ok := m.selectedCommandSuggestion()
	if !ok {
		t.Fatalf("expected a selected command in suggestion table")
	}
	if selected != "/quit" {
		t.Fatalf("expected down arrow to select /quit, got %q", selected)
	}

	var cmd tea.Cmd
	m, cmd = applyUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected enter to return quit command for /quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected enter on selected /quit to trigger quit command")
	}

	if got := m.input.Value(); got != "/quit" {
		t.Fatalf("expected selected command to be applied before sending, got %q", got)
	}
	if m.commandSuggestionsShown {
		t.Fatalf("expected suggestion table to hide after applying selection")
	}
}

func TestTypingHidesSuggestionTable(t *testing.T) {
	m := newSizedTestModel(t)
	m.input.SetValue("/")
	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	if m.commandSuggestionsShown {
		t.Fatalf("expected typing to hide suggestion table")
	}
	if got := m.input.Value(); got != "/c" {
		t.Fatalf("expected typed input to continue, got %q", got)
	}
}

func TestCommandSuggestionsForPrefixDelegatesToSharedSuggestions(t *testing.T) {
	got := commandSuggestionsForPrefix("/c")
	want := tuicmd.SuggestionsForPrefix("/c")

	if len(got) != len(want) {
		t.Fatalf("expected %d suggestions, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i].Command != want[i].Command {
			t.Fatalf("command mismatch at %d: got %q, want %q", i, got[i].Command, want[i].Command)
		}
		if got[i].Description != want[i].Description {
			t.Fatalf("description mismatch at %d: got %q, want %q", i, got[i].Description, want[i].Description)
		}
	}
}

func TestSplitSlashCommandInputUsesSharedParser(t *testing.T) {
	cmd, suffix, ok := splitSlashCommandInput("/compact\t100")
	if !ok {
		t.Fatal("expected slash command parsing to succeed")
	}
	if cmd != "/compact" {
		t.Fatalf("expected command %q, got %q", "/compact", cmd)
	}
	if suffix != "\t100" {
		t.Fatalf("expected suffix %q, got %q", "\\t100", suffix)
	}

	_, wantSuffix, wantOK := tuicmd.SplitSlashCommandInput("/compact\t100")
	if !wantOK {
		t.Fatal("expected shared parser to accept input")
	}
	if suffix != wantSuffix {
		t.Fatalf("expected wrapper suffix %q to match shared parser %q", suffix, wantSuffix)
	}
}
