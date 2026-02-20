package bubbletea

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m model) View() string {
	width := max(0, m.width)
	header := m.headerStyle.Width(width).Render(m.headerLine())
	subHeader := m.subHeaderStyle.Width(width).Render(m.subHeaderLine())
	body := m.bodyStyle.Render(m.vp.View())
	input := m.renderInputArea()

	lines := []string{
		header,
		subHeader,
		"",
		body,
		"",
		input,
	}
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
		right = "streaming..."
	}

	leftStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	rightStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#95A3B8"))
	if m.streaming {
		rightStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("179")).Bold(true)
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
