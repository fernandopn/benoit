package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	glamourstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m model) renderTranscript() string {
	var b strings.Builder
	rendered := 0
	for _, block := range m.blocks {
		segment := m.renderBlock(block)
		if strings.TrimSpace(segment) == "" {
			continue
		}
		if rendered > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(segment)
		rendered++
	}
	content := strings.TrimSpace(b.String())
	if m.vp.Width > 0 {
		return lipgloss.NewStyle().Width(m.vp.Width).Render(content)
	}
	return content
}

func (m model) renderBlock(b block) string {
	switch b.Kind {
	case blockUser:
		return m.renderUserBlock(b.Text)
	case blockAssistant:
		return renderMarkdown(b.Text, m.assistantMarkdownRender)
	case blockReasoning:
		return renderMarkdown(b.Text, m.reasoningMarkdownRender)
	case blockToolCall:
		return renderToolBlock(m.toolLabelStyle.Render("Tool Call"), b.Meta, b.Text, "args")
	case blockToolResult:
		return renderToolBlock(m.toolLabelStyle.Render("Tool Result"), b.Meta, b.Text, "output")
	case blockToolWidget:
		return m.renderToolWidget(b)
	case blockContext:
		return m.contextLabelStyle.Render("Context Usage") + "\n" + m.contextTextStyle.Render(formatContextUsage(b.Text, b.Meta))
	case blockError:
		return m.errorLabelStyle.Render("Error") + "\n" + m.errorTextStyle.Render(b.Text)
	case blockSystem:
		return m.systemTextStyle.Render(b.Text)
	default:
		return b.Text
	}
}

func (m model) renderUserBlock(text string) string {
	width := m.vp.Width
	if width <= 0 {
		return m.userTextStyle.Render(text)
	}
	if width <= 2 {
		return m.inputBgStyle.Render(strings.Repeat(" ", width))
	}

	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}

	bgPrefix := m.inputBgPrefix()
	leftPad := m.inputBgStyle.Render(" ")
	rightPad := m.inputBgStyle.Render(" ")
	innerWidth := width - 2
	blank := leftPad + m.inputBgStyle.Render(strings.Repeat(" ", innerWidth)) + rightPad
	out := make([]string, 0, len(lines)+2)
	out = append(out, blank)

	for _, line := range lines {
		raw := line
		if ansi.StringWidth(raw) > innerWidth {
			raw = ansi.Truncate(raw, innerWidth, "")
		}
		styled := m.userTextStyle.Render(raw)
		styled = applyLineBackground(styled, bgPrefix)
		pad := innerWidth - ansi.StringWidth(styled)
		if pad < 0 {
			pad = 0
		}
		if pad > 0 {
			styled += m.inputBgStyle.Render(strings.Repeat(" ", pad))
		}
		out = append(out, leftPad+styled+rightPad)
	}

	out = append(out, blank)
	return strings.Join(out, "\n")
}

func (m model) renderToolWidget(b block) string {
	width := m.vp.Width
	if width <= 0 {
		width = 80
	}
	boxWidth := width
	if boxWidth > 2 {
		boxWidth = width - 2
	}
	contentWidth := boxWidth - 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	toolName := ""
	if b.Meta != nil {
		toolName = b.Meta["tool"]
	}
	if toolName == "" {
		toolName = "tool"
	}

	args := compactWhitespace(strings.TrimSpace(b.ToolArgs))
	result := strings.TrimSpace(b.ToolResult)

	if args == "" {
		args = "{}"
	}
	if result == "" {
		result = m.toolSpinnerLabel()
	}

	header := m.toolNameStyle.Render(toolName) + " (" + m.toolBodyStyle.Render(args) + ")"
	bodyLines := []string{
		header,
		m.toolBodyStyle.Render(result),
	}
	body := lipgloss.NewStyle().Width(contentWidth).Render(strings.Join(bodyLines, "\n"))

	return m.toolBoxStyle.Width(boxWidth).Render(body)
}

func (m model) toolSpinnerLabel() string {
	if !m.toolSpinnerActive || len(m.toolSpinnerFrames) == 0 {
		return "Running..."
	}
	frame := m.toolSpinnerFrames[m.toolSpinnerIndex%len(m.toolSpinnerFrames)]
	return "Running " + frame
}

func compactWhitespace(value string) string {
	if value == "" {
		return ""
	}
	return strings.Join(strings.Fields(value), " ")
}

func assistantMarkdownStyleConfig() glamouransi.StyleConfig {
	return glamourstyles.DarkStyleConfig
}

func reasoningMarkdownStyleConfig(baseColor lipgloss.Color) glamouransi.StyleConfig {
	style := glamourstyles.DarkStyleConfig
	applyMarkdownBase(&style, string(baseColor), true)
	return style
}

func applyMarkdownBase(style *glamouransi.StyleConfig, color string, italic bool) {
	colorPtr := strPtr(color)

	applyPrimitive(&style.Text, colorPtr, italic)
	applyPrimitive(&style.Emph, colorPtr, italic)
	applyPrimitive(&style.Strong, colorPtr, italic)
	applyPrimitive(&style.Strikethrough, colorPtr, italic)
	applyPrimitive(&style.Paragraph.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.Document.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.BlockQuote.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.List.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.Item, colorPtr, italic)
	applyPrimitive(&style.Enumeration, colorPtr, italic)
	applyPrimitive(&style.Task.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.Heading.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.H1.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.H2.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.H3.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.H4.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.H5.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.H6.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.Link, colorPtr, italic)
	applyPrimitive(&style.LinkText, colorPtr, italic)
	applyPrimitive(&style.HorizontalRule, colorPtr, italic)
	applyPrimitive(&style.Table.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.DefinitionList.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.DefinitionTerm, colorPtr, italic)
	applyPrimitive(&style.DefinitionDescription, colorPtr, italic)
	applyPrimitive(&style.Image, colorPtr, italic)
	applyPrimitive(&style.ImageText, colorPtr, italic)
	applyPrimitive(&style.HTMLBlock.StylePrimitive, colorPtr, italic)
	applyPrimitive(&style.HTMLSpan.StylePrimitive, colorPtr, italic)
}

func applyPrimitive(target *glamouransi.StylePrimitive, color *string, italic bool) {
	if color != nil {
		target.Color = color
	}
	if italic {
		target.Italic = boolPtr(true)
	}
}

func (m *model) updateMarkdownRenderers() {
	width := m.vp.Width
	if width <= 0 {
		width = 80
	}
	if width == m.markdownWidth && m.assistantMarkdownRender != nil && m.reasoningMarkdownRender != nil {
		return
	}
	m.assistantMarkdownRender = newMarkdownRenderer(width, m.assistantMarkdownStyle)
	m.reasoningMarkdownRender = newMarkdownRenderer(width, m.reasoningMarkdownStyle)
	m.markdownWidth = width
}

func newMarkdownRenderer(width int, style glamouransi.StyleConfig) *glamour.TermRenderer {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(width),
		glamour.WithTableWrap(true),
		glamour.WithInlineTableLinks(true),
	)
	if err != nil {
		return nil
	}
	return renderer
}

func renderMarkdown(text string, renderer *glamour.TermRenderer) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if renderer == nil {
		return strings.TrimSpace(text)
	}
	rendered, err := renderer.Render(text)
	if err != nil {
		return strings.TrimSpace(text)
	}
	return strings.TrimRight(rendered, "\n")
}

func strPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func renderToolBlock(title string, meta map[string]string, body string, bodyLabel string) string {
	var b strings.Builder
	b.WriteString(title)

	details := []string{}
	if meta != nil {
		if tool := meta["tool"]; tool != "" {
			details = append(details, "tool: "+tool)
		}
		if id := meta["call_id"]; id != "" {
			details = append(details, "id: "+id)
		}
	}
	if len(details) > 0 {
		b.WriteString("\n")
		b.WriteString(indentLines(strings.Join(details, "\n"), "  "))
	}

	body = strings.TrimSpace(body)
	if body != "" {
		b.WriteString("\n  " + bodyLabel + ":\n")
		b.WriteString(indentLines(body, "    "))
	}
	return b.String()
}

func formatContextUsage(text string, meta map[string]string) string {
	var b strings.Builder
	base := strings.TrimSpace(text)
	if base != "" {
		b.WriteString(base)
	}
	if meta != nil {
		used := meta["tokens_used"]
		available := meta["tokens_available"]
		if used != "" || available != "" {
			if b.Len() > 0 {
				b.WriteString(" ")
			}
			if used != "" {
				b.WriteString("tokens_used=" + used)
			}
			if available != "" {
				if used != "" {
					b.WriteString(" ")
				}
				b.WriteString("tokens_available=" + available)
			}
		}
	}
	return b.String()
}

func indentLines(text, prefix string) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
