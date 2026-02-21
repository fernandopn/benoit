package bubbletea

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m model) View() string {
	width := max(0, m.width)
	header := m.headerStyle.Width(width).Render(m.headerLine())
	subHeader := m.subHeaderStyle.Width(width).Render(m.subHeaderLine())
	body := m.bodyStyle.Render(m.vp.View())

	lines := []string{
		header,
		subHeader,
		"",
		body,
		"",
	}
	if suggestions := m.renderCommandSuggestions(); suggestions != "" {
		lines = append(lines, suggestions)
	}
	lines = append(lines, m.renderInputArea())
	lines = append(lines, m.footerLine())
	return strings.Join(lines, "\n")
}

func (m model) subHeaderLine() string {
	return m.helpText
}

func (m model) headerLine() string {
	left := "Benoit"
	if strings.TrimSpace(m.providerName) != "" {
		left += " · " + m.providerName
	}
	right := "ready"
	if m.streaming {
		right = "streaming"
	} else if m.hasPendingToolResults() {
		right = "tools"
	}

	leftStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	rightStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#95A3B8"))
	if m.streaming {
		rightStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("179")).Bold(true)
	} else if m.hasPendingToolResults() {
		rightStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#8FB3FF")).Bold(true)
	}

	width := max(0, m.width-2)
	rightText := rightStyle.Render(right)
	rightWidth := lipgloss.Width(rightText)
	leftMax := width - rightWidth - 1
	if leftMax < 0 {
		leftMax = 0
	}
	leftRaw := left
	if leftMax > 0 {
		leftRaw = lipgloss.NewStyle().MaxWidth(leftMax).Render(left)
	} else {
		leftRaw = ""
	}
	leftText := leftStyle.Render(leftRaw)
	gap := width - lipgloss.Width(leftText) - rightWidth
	if gap < 1 {
		gap = 1
	}
	line := leftText + strings.Repeat(" ", gap) + rightText
	if pad := width - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line
}

func (m *model) updateContextUsage(value string, meta map[string]string) {
	left, ok := contextLeftPercent(value, meta)
	if !ok {
		return
	}
	m.contextLeftPercent = clampPercent(left)
	m.contextLeftKnown = true
	m.contextTokensUsed = ""
	m.contextTokensTotal = ""

	if meta == nil {
		return
	}
	m.contextTokensUsed = strings.TrimSpace(meta["tokens_input_used"])
	if m.contextTokensUsed == "" {
		m.contextTokensUsed = strings.TrimSpace(meta["tokens_used"])
	}
	m.contextTokensTotal = strings.TrimSpace(meta["tokens_available"])
}

func (m model) footerLine() string {
	width := max(0, m.width)
	if !m.contextLeftKnown || width == 0 {
		return strings.Repeat(" ", width)
	}

	tone := contextToneColor(m.contextLeftPercent)
	fillStyle := lipgloss.NewStyle().Foreground(tone)
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#334155"))
	textStyle := lipgloss.NewStyle().Foreground(tone).Bold(true)

	barWidth := width / 4
	if barWidth < 10 {
		barWidth = 10
	}
	if barWidth > 28 {
		barWidth = 28
	}
	filled := int(math.Round((m.contextLeftPercent / 100) * float64(barWidth)))
	if filled < 0 {
		filled = 0
	}
	if filled > barWidth {
		filled = barWidth
	}
	bar := "[" + fillStyle.Render(strings.Repeat("=", filled)) + emptyStyle.Render(strings.Repeat("-", barWidth-filled)) + "]"

	leftText := fmt.Sprintf("%.1f%% left", m.contextLeftPercent)
	if m.contextLeftPercent >= 99.95 {
		leftText = "100% left"
	}

	used := compactTokenCount(m.contextTokensUsed)
	total := compactTokenCount(m.contextTokensTotal)
	if used != "" && total != "" {
		leftText += " " + used + "/" + total
	}

	content := bar + " " + textStyle.Render(leftText)
	if lipgloss.Width(content) > width {
		content = ansi.Truncate(content, width, "")
	}
	if pad := width - lipgloss.Width(content); pad > 0 {
		return strings.Repeat(" ", pad) + content
	}
	return content
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func contextToneColor(leftPercent float64) lipgloss.Color {
	if leftPercent <= 10 {
		return lipgloss.Color("#E27C7C")
	}
	if leftPercent <= 30 {
		return lipgloss.Color("#E0B36A")
	}
	return lipgloss.Color("#87C994")
}

func compactTokenCount(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	num, ok := parseFloatLoose(value)
	if !ok {
		return value
	}
	switch {
	case num >= 1_000_000:
		return fmt.Sprintf("%.1fm", num/1_000_000)
	case num >= 10_000:
		return fmt.Sprintf("%.0fk", num/1_000)
	case num >= 1_000:
		return fmt.Sprintf("%.1fk", num/1_000)
	default:
		return fmt.Sprintf("%.0f", num)
	}
}
