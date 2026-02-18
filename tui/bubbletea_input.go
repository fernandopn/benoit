package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

func (m *model) updateContextUsage(value string, meta map[string]string) {
	left, ok := contextLeftPercent(value, meta)
	if !ok {
		return
	}
	if left < 0 {
		left = 0
	}
	if left > 100 {
		left = 100
	}
	if left >= 99.95 {
		m.contextLeft = "100% context left"
		return
	}
	m.contextLeft = fmt.Sprintf("%.1f%% context left", left)
}

func contextLeftPercent(value string, meta map[string]string) (float64, bool) {
	if percentUsed, ok := parsePercent(value); ok {
		return 100 - percentUsed, true
	}
	if meta != nil {
		used, usedOK := parseFloatLoose(meta["tokens_used"])
		avail, availOK := parseFloatLoose(meta["tokens_available"])
		if usedOK && availOK {
			if avail <= 0 {
				return 0, false
			}
			if avail < used {
				total := used + avail
				if total > 0 {
					return (avail / total) * 100, true
				}
			}
			return ((avail - used) / avail) * 100, true
		}
	}
	return 0, false
}

func parseFloatLoose(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	value = strings.ReplaceAll(value, ",", "")
	num, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return num, true
}

func parsePercent(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "%")
	if value == "" {
		return 0, false
	}
	return parseFloatLoose(value)
}

func (m model) footerLine() string {
	width := max(0, m.width)
	if m.contextLeft == "" {
		return strings.Repeat(" ", width)
	}
	text := m.contextLeftStyle.Render(m.contextLeft)
	pad := width - lipgloss.Width(text)
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + text
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

	inputAreaHeight := m.input.Height() + 2 + inputFrameH
	viewportHeight := m.height - headerHeight - gapHeight*2 - inputAreaHeight - bodyFrameH - footerHeight
	m.vp.Width = max(10, m.width-bodyFrameW)
	m.vp.Height = max(1, viewportHeight)
	m.updateMarkdownRenderers()

	wasAtBottom := m.vp.AtBottom()
	m.vp.SetContent(m.renderTranscript())
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
