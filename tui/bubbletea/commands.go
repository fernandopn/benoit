package bubbletea

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tuicmd "github.com/fernandopn/benoit/tui/commands"
)

func splitSlashCommandInput(value string) (string, string, bool) {
	return tuicmd.SplitSlashCommandInput(value)
}

func commandSuggestionsForPrefix(prefix string) []commandSuggestion {
	return tuicmd.SuggestionsForPrefix(prefix)
}

func (m *model) showCommandSuggestions(prefix string, suggestions []commandSuggestion) {
	rows := make([]table.Row, 0, len(suggestions))
	for _, suggestion := range suggestions {
		rows = append(rows, table.Row{suggestion.Command, suggestion.Description})
	}
	m.commandSuggestions = append([]commandSuggestion(nil), suggestions...)
	m.commandSuggestionsShown = len(rows) > 1
	m.commandCompletionPrefix = prefix
	m.cmds.SetRows(rows)
	m.cmds.SetCursor(0)
	m.relayout(false)
}

func (m *model) hideCommandSuggestions() {
	if !m.commandSuggestionsShown && len(m.commandSuggestions) == 0 {
		return
	}
	m.commandSuggestions = nil
	m.commandSuggestionsShown = false
	m.commandCompletionPrefix = ""
	m.cmds.SetRows(nil)
	m.cmds.SetCursor(0)
	m.relayout(false)
}

func (m *model) applyCommandCompletion(command, suffix string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	m.input.SetValue(command + suffix)
	m.adjustInputHeight()
}

func (m model) selectedCommandSuggestion() (string, bool) {
	row := m.cmds.SelectedRow()
	if len(row) == 0 {
		return "", false
	}
	command := strings.TrimSpace(row[0])
	if command == "" {
		return "", false
	}
	return command, true
}

func (m *model) updateCommandTableLayout(contentWidth int, maxVisibleLines int) {
	if contentWidth <= 0 {
		return
	}
	tableWidth := max(10, contentWidth)
	m.cmds.SetWidth(tableWidth)

	commandWidth := 14
	descriptionWidth := tableWidth - commandWidth - 3
	if descriptionWidth < 10 {
		descriptionWidth = 10
		commandWidth = tableWidth - descriptionWidth - 3
		if commandWidth < 8 {
			commandWidth = 8
			descriptionWidth = max(10, tableWidth-commandWidth-3)
		}
	}
	m.cmds.SetColumns([]table.Column{
		{Title: "Command", Width: commandWidth},
		{Title: "Description", Width: descriptionWidth},
	})

	height := 1
	if len(m.commandSuggestions) > 0 {
		height = max(commandSuggestionMinRows, len(m.commandSuggestions)) + 1
	}
	if maxVisibleLines > 0 {
		maxRows := maxVisibleLines - 2
		if maxRows < 1 {
			maxRows = 1
		}
		if height > maxRows {
			height = maxRows
		}
	}
	m.cmds.SetHeight(height)
	m.cmds.UpdateViewport()
}

func (m model) commandSuggestionHeight() int {
	if !m.commandSuggestionsShown {
		return 0
	}
	view := strings.TrimRight(m.cmds.View(), "\n")
	if view == "" {
		return 0
	}
	return countLines(view)
}

func (m model) renderCommandSuggestions() string {
	if !m.commandSuggestionsShown {
		return ""
	}
	return strings.TrimRight(m.cmds.View(), "\n")
}
