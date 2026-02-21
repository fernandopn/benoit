package bubbletea

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func isScrollKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
		return true
	case tea.KeyCtrlU, tea.KeyCtrlD:
		return true
	default:
		return false
	}
}

func isMouseEscapeKey(msg tea.KeyMsg) bool {
	if msg.Type != tea.KeyRunes || len(msg.Runes) == 0 {
		return false
	}

	raw := string(msg.Runes)
	if strings.ContainsRune(raw, '\x1b') {
		seq := raw[strings.IndexRune(raw, '\x1b'):]
		if looksLikeMouseCSI(seq) {
			return true
		}
	}

	if looksLikeMouseCSI(raw) {
		return true
	}

	return false
}

func looksLikeMouseCSI(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	raw = strings.TrimPrefix(raw, "\x1b")

	if strings.HasPrefix(raw, "[M") && len(raw) >= 6 {
		return true
	}
	if strings.HasPrefix(raw, "[<") && strings.Count(raw, ";") >= 2 {
		last := raw[len(raw)-1]
		if last == 'M' || last == 'm' {
			return true
		}
	}
	return false
}

func batchCmds(cmds []tea.Cmd) tea.Cmd {
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *model) handleCommandKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp, tea.KeyDown:
		if !m.commandSuggestionsShown {
			return false, nil
		}
		var cmd tea.Cmd
		m.cmds, cmd = m.cmds.Update(msg)
		return true, cmd
	case tea.KeyTab:
		return m.completeSlashCommand()
	default:
		return false, nil
	}
}

func (m *model) completeSlashCommand() (bool, tea.Cmd) {
	prefix, suffix, ok := splitSlashCommandInput(m.input.Value())
	if !ok {
		if m.commandSuggestionsShown {
			m.hideCommandSuggestions()
		}
		return false, nil
	}

	matches := commandSuggestionsForPrefix(prefix)
	if len(matches) == 0 {
		m.hideCommandSuggestions()
		return true, nil
	}

	if len(matches) == 1 {
		m.applyCommandCompletion(matches[0].Command, suffix)
		m.hideCommandSuggestions()
		return true, nil
	}

	if m.commandSuggestionsShown && strings.EqualFold(strings.TrimSpace(m.commandCompletionPrefix), strings.TrimSpace(prefix)) {
		if selected, ok := m.selectedCommandSuggestion(); ok {
			m.applyCommandCompletion(selected, suffix)
			m.hideCommandSuggestions()
			return true, nil
		}
	}

	m.showCommandSuggestions(prefix, matches)
	return true, nil
}

func isSoftNewlineMsg(msg tea.Msg) bool {
	if km, ok := msg.(tea.KeyMsg); ok {
		if km.String() == "shift+enter" {
			return true
		}
		if km.Type == tea.KeyEnter && km.Alt {
			return true
		}
		if km.Type == tea.KeyCtrlJ {
			return true
		}
		if km.Type == tea.KeyCtrlM && km.Alt {
			return true
		}
		if km.Type == tea.KeyRunes && len(km.Runes) == 1 && km.Runes[0] == '\n' {
			return true
		}
	}

	if seq, ok := decodeUnknownCSI(msg); ok {
		if isShiftEnterSeq(seq) {
			return true
		}
	}
	return false
}

func isShiftEnterSeq(seq string) bool {
	nums := extractCSIInts(seq)
	if len(nums) == 0 {
		return false
	}
	hasEnter := false
	hasShift := false
	for _, n := range nums {
		if n == 13 {
			hasEnter = true
		}
		if n == 2 {
			hasShift = true
		}
	}
	return hasEnter && hasShift
}

func extractCSIInts(seq string) []int {
	var nums []int
	n := 0
	inNum := false
	for _, r := range seq {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
			inNum = true
			continue
		}
		if inNum {
			nums = append(nums, n)
			n = 0
			inNum = false
		}
	}
	if inNum {
		nums = append(nums, n)
	}
	return nums
}

func decodeUnknownCSI(msg tea.Msg) (string, bool) {
	stringer, ok := msg.(fmt.Stringer)
	if !ok {
		return "", false
	}
	text := stringer.String()
	if !strings.HasPrefix(text, "?CSI[") || !strings.HasSuffix(text, "]?") {
		return "", false
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(text, "?CSI["), "]?")
	fields := strings.Fields(payload)
	if len(fields) == 0 {
		return "", false
	}
	bytes := make([]byte, 0, len(fields))
	for _, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 || n > 255 {
			return "", false
		}
		bytes = append(bytes, byte(n))
	}
	return string(bytes), true
}

func (m *model) expandToolResultAt(mouse tea.MouseMsg) bool {
	if mouse.Button != tea.MouseButtonLeft {
		return false
	}
	if mouse.Action != tea.MouseActionPress && mouse.Action != tea.MouseActionRelease {
		return false
	}

	line, col, ok := m.transcriptCoordinateFromMouse(mouse.X, mouse.Y)
	if !ok {
		return false
	}

	for _, target := range m.toolExpandTargets {
		if target.Line != line {
			continue
		}
		if col < target.ColStart || col >= target.ColEnd {
			continue
		}
		if target.BlockIndex < 0 || target.BlockIndex >= len(m.blocks) {
			return false
		}
		if m.blocks[target.BlockIndex].Kind != blockToolWidget {
			return false
		}
		if m.blocks[target.BlockIndex].ToolResultExpanded {
			return false
		}
		m.blocks[target.BlockIndex].ToolResultExpanded = true
		return true
	}

	return false
}

func (m model) transcriptCoordinateFromMouse(mouseX, mouseY int) (int, int, bool) {
	startX, startY := m.viewportContentOrigin()
	if mouseX < startX || mouseY < startY {
		return 0, 0, false
	}
	row := mouseY - startY
	if row < 0 || row >= m.vp.Height {
		return 0, 0, false
	}
	col := mouseX - startX
	if col < 0 {
		return 0, 0, false
	}
	return m.vp.YOffset + row, col, true
}

func (m model) viewportContentOrigin() (int, int) {
	width := max(0, m.width)
	header := m.headerStyle.Width(width).Render(m.headerLine())
	subHeader := m.subHeaderStyle.Width(width).Render(m.subHeaderLine())
	prefixLines := countLines(header) + countLines(subHeader) + 1

	startX := m.bodyStyle.GetMarginLeft() + m.bodyStyle.GetBorderLeftSize() + m.bodyStyle.GetPaddingLeft()
	startY := prefixLines + m.bodyStyle.GetMarginTop() + m.bodyStyle.GetBorderTopSize() + m.bodyStyle.GetPaddingTop()
	return startX, startY
}

func countLines(value string) int {
	if value == "" {
		return 1
	}
	return strings.Count(value, "\n") + 1
}

func (m *model) adjustInputHeight() {
	if m.width == 0 || m.height == 0 {
		return
	}
	lines := strings.Count(m.input.Value(), "\n") + 1
	if lines < 1 {
		lines = 1
	}
	if lines > m.input.MaxHeight {
		lines = m.input.MaxHeight
	}
	if lines != m.input.Height() {
		m.input.SetHeight(lines)
		m.relayout(false)
	}
}

func (m *model) prepareSoftNewline() {
	lines := strings.Count(m.input.Value(), "\n") + 1
	nextLines := lines + 1
	if nextLines > m.input.MaxHeight {
		nextLines = m.input.MaxHeight
	}
	if nextLines > m.input.Height() {
		m.input.SetHeight(nextLines)
		m.relayout(false)
	}
}

func (m *model) relayout(followBottom bool) {
	if m.width == 0 || m.height == 0 {
		return
	}
	headerHeight := 2
	gapHeight := 1
	footerHeight := 1

	bodyFrameW, bodyFrameH := m.bodyStyle.GetFrameSize()
	inputFrameW, inputFrameH := m.inputBoxStyle.GetFrameSize()

	contentWidth := max(10, m.width-2-inputFrameW)
	m.input.SetWidth(contentWidth)

	inputCoreHeight := m.input.Height() + 2 + inputFrameH
	maxSuggestionLines := m.height - headerHeight - gapHeight*2 - inputCoreHeight - bodyFrameH - footerHeight - 1
	if maxSuggestionLines < 1 {
		maxSuggestionLines = 1
	}
	m.updateCommandTableLayout(contentWidth, maxSuggestionLines)

	inputAreaHeight := inputCoreHeight + m.commandSuggestionHeight()
	viewportHeight := m.height - headerHeight - gapHeight*2 - inputAreaHeight - bodyFrameH - footerHeight
	m.vp.Width = max(10, m.width-bodyFrameW)
	m.vp.Height = max(1, viewportHeight)
	m.updateMarkdownRenderers()

	wasAtBottom := m.vp.AtBottom()
	m.refreshTranscript()
	if followBottom || wasAtBottom {
		m.vp.GotoBottom()
	}
}

func (m model) renderInputArea() string {
	width := max(0, m.width)
	contentWidth := max(0, width-2)
	border := m.inputBgStyle.Render(strings.Repeat(" ", width))
	lines := strings.Split(m.input.View(), "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	bgPrefix := m.inputBgPrefix()
	for i := range lines {
		line := lines[i]
		if contentWidth > 0 && ansi.StringWidth(line) > contentWidth {
			line = ansi.Truncate(line, contentWidth, "")
		}
		line = applyLineBackground(line, bgPrefix)
		pad := contentWidth - ansi.StringWidth(line)
		if pad < 0 {
			pad = 0
		}
		lines[i] = m.inputBgStyle.Render(" ") + line + m.inputBgStyle.Render(strings.Repeat(" ", pad)) + m.inputBgStyle.Render(" ")
	}
	return strings.Join(append([]string{border}, append(lines, border)...), "\n")
}

func (m model) inputBgPrefix() string {
	sample := m.inputBgStyle.Render(" ")
	idx := strings.Index(sample, " ")
	if idx == -1 {
		return ""
	}
	return sample[:idx]
}

func applyLineBackground(line, bgPrefix string) string {
	if bgPrefix == "" {
		return line
	}
	out := bgPrefix + line
	out = strings.ReplaceAll(out, "\x1b[0m", "\x1b[0m"+bgPrefix)
	return out + "\x1b[0m"
}
