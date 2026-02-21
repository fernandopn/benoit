package bubbletea

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	streamReadMaxWait = 25 * time.Millisecond
	streamReadMaxMsgs = 64
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if isSoftNewlineMsg(msg) {
		m.prepareSoftNewline()
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\n'}}
	}

	var (
		cmds []tea.Cmd
		cmd  tea.Cmd
	)

	switch typed := msg.(type) {
	case tea.KeyMsg:
		if isScrollKey(typed) {
			m.vp, cmd = m.vp.Update(msg)
			cmds = append(cmds, cmd)
		}
		handled, keyCmd := m.handleCommandKey(typed)
		if handled {
			cmds = append(cmds, keyCmd)
			m.adjustInputHeight()
			return m, batchCmds(cmds)
		}
		beforeInput := m.input.Value()
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
		if m.commandSuggestionsShown && m.input.Value() != beforeInput {
			m.hideCommandSuggestions()
		}
	default:
		m.vp, cmd = m.vp.Update(msg)
		cmds = append(cmds, cmd)
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	m.adjustInputHeight()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		m.relayout(true)
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			m.cancelStreamIfAny()
			return m, tea.Quit
		case tea.KeyEnter:
			if m.streaming {
				return m, batchCmds(cmds)
			}
			if m.commandSuggestionsShown {
				if selected, ok := m.selectedCommandSuggestion(); ok {
					_, suffix, split := splitSlashCommandInput(m.input.Value())
					if !split {
						suffix = ""
					}
					m.applyCommandCompletion(selected, suffix)
				}
				m.hideCommandSuggestions()
			}
			raw := m.input.Value()
			if strings.TrimSpace(raw) == "" {
				return m, nil
			}
			prompt := strings.TrimRight(raw, "\n")
			if prompt == "/exit" || prompt == "/quit" {
				m.cancelStreamIfAny()
				return m, tea.Quit
			}

			m.cancelStreamIfAny()
			m.hideCommandSuggestions()
			m.blocks = append(m.blocks, block{Kind: blockUser, Text: prompt})
			m.input.Reset()

			m.refreshTranscript()
			m.vp.GotoBottom()

			m.streaming = true
			m.streamSeq++
			m.activeSeq = m.streamSeq
			return m, tea.Batch(append(cmds, startStream(m.ctx, m.startStream, prompt, m.activeSeq))...)
		}

	case streamStartFailedMsg:
		if msg.Seq != m.activeSeq {
			if msg.Cancel != nil {
				msg.Cancel()
			}
			return m, batchCmds(cmds)
		}
		if msg.Cancel != nil {
			msg.Cancel()
		}
		m.streaming = false
		if msg.Err != nil {
			wasAtBottom := m.vp.AtBottom()
			m.appendBlock(blockError, msg.Err.Error(), nil)
			m.refreshTranscript()
			if wasAtBottom {
				m.vp.GotoBottom()
			}
		}
		cmds = append(cmds, m.syncToolSpinner())
		return m, batchCmds(cmds)

	case streamStartedMsg:
		if msg.Seq != m.activeSeq {
			if msg.Cancel != nil {
				msg.Cancel()
			}
			return m, batchCmds(cmds)
		}
		m.streamCh = msg.Ch
		m.streamCancel = msg.Cancel
		return m, tea.Batch(append(cmds, readStreamChunk(m.streamCh, streamReadMaxWait, streamReadMaxMsgs, msg.Seq))...)

	case streamChunkMsg:
		if msg.Seq != m.activeSeq {
			return m, batchCmds(cmds)
		}

		wasAtBottom := m.vp.AtBottom()
		if len(msg.Msgs) > 0 {
			m.applyStreamMessages(msg.Msgs)
			m.refreshTranscript()
		}
		if wasAtBottom {
			m.vp.GotoBottom()
		}

		if msg.Done {
			m.streaming = false
			m.cancelStreamIfAny()
			cmds = append(cmds, m.syncToolSpinner())
			return m, batchCmds(cmds)
		}
		cmds = append(cmds, m.syncToolSpinner())
		return m, tea.Batch(append(cmds, readStreamChunk(m.streamCh, streamReadMaxWait, streamReadMaxMsgs, msg.Seq))...)
	case toolSpinnerTick:
		if !m.toolSpinnerActive {
			return m, batchCmds(cmds)
		}
		if !m.hasPendingToolResults() {
			m.toolSpinnerActive = false
			return m, batchCmds(cmds)
		}
		if len(m.toolSpinnerFrames) > 0 {
			m.toolSpinnerIndex = (m.toolSpinnerIndex + 1) % len(m.toolSpinnerFrames)
		}
		wasAtBottom := m.vp.AtBottom()
		m.refreshTranscript()
		if wasAtBottom {
			m.vp.GotoBottom()
		}
		cmds = append(cmds, m.nextToolSpinnerTick())
		return m, batchCmds(cmds)

	case tea.MouseMsg:
		if m.expandToolResultAt(msg) {
			wasAtBottom := m.vp.AtBottom()
			m.refreshTranscript()
			if wasAtBottom {
				m.vp.GotoBottom()
			}
		}
		return m, batchCmds(cmds)
	}

	return m, batchCmds(cmds)
}
