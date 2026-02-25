package bubbletea

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	glamourstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m *model) refreshTranscript() {
	content, targets := m.renderTranscriptWithTargets()
	m.toolExpandTargets = targets
	m.vp.SetContent(content)
}

func (m model) renderTranscript() string {
	content, _ := m.renderTranscriptWithTargets()
	return content
}

func (m model) renderTranscriptWithTargets() (string, []toolExpandTarget) {
	var b strings.Builder
	rendered := 0
	expandableBlocks := make([]int, 0)
	for i := range m.blocks {
		segment, expandable := m.renderBlock(m.blocks[i])
		if strings.TrimSpace(segment) == "" {
			continue
		}
		if rendered > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(segment)
		if expandable {
			expandableBlocks = append(expandableBlocks, i)
		}
		rendered++
	}
	content := strings.TrimSpace(b.String())
	if m.vp.Width > 0 {
		content = lipgloss.NewStyle().Width(m.vp.Width).Render(content)
	}
	return content, locateToolExpandTargets(content, expandableBlocks)
}

func locateToolExpandTargets(content string, blockIndexes []int) []toolExpandTarget {
	if len(blockIndexes) == 0 || strings.TrimSpace(content) == "" {
		return nil
	}

	lines := strings.Split(content, "\n")
	targets := make([]toolExpandTarget, 0, len(blockIndexes))
	blockPos := 0

	for lineIndex, line := range lines {
		if blockPos >= len(blockIndexes) {
			break
		}

		plain := ansi.Strip(line)
		searchFrom := 0
		for blockPos < len(blockIndexes) && searchFrom <= len(plain) {
			rel := strings.Index(plain[searchFrom:], toolResultExpandLabel)
			if rel < 0 {
				break
			}
			idx := searchFrom + rel
			colStart := ansi.StringWidth(plain[:idx])
			colEnd := colStart + ansi.StringWidth(toolResultExpandLabel)
			targets = append(targets, toolExpandTarget{
				BlockIndex: blockIndexes[blockPos],
				Line:       lineIndex,
				ColStart:   colStart,
				ColEnd:     colEnd,
			})
			blockPos++
			searchFrom = idx + len(toolResultExpandLabel)
		}
	}

	return targets
}

func (m model) renderBlock(b block) (string, bool) {
	switch b.Kind {
	case blockUser:
		return m.renderUserBlock(b.Text), false
	case blockAssistant:
		return m.renderAssistantBlock(b.Text), false
	case blockReasoning:
		return m.renderReasoningBlock(b.Text), false
	case blockToolCall:
		return m.renderToolWidget(block{
			Kind:      blockToolWidget,
			Meta:      cloneMeta(b.Meta),
			ToolArgs:  b.Text,
			ToolState: toolExecutionPending,
		})
	case blockToolResult:
		return m.renderToolWidget(block{
			Kind:       blockToolWidget,
			Meta:       cloneMeta(b.Meta),
			ToolResult: b.Text,
			ToolState:  toolExecutionDone,
		})
	case blockToolWidget:
		return m.renderToolWidget(b)
	case blockContext:
		return m.contextTextStyle.Render(formatContextUsage(b.Text, b.Meta)), false
	case blockError:
		return m.renderErrorBlock(b.Text), false
	case blockSystem:
		return m.systemTextStyle.Render(b.Text), false
	default:
		return b.Text, false
	}
}

func (m model) renderAssistantBlock(text string) string {
	return m.renderCard(renderMarkdown(text, m.assistantMarkdownRender), m.assistantCardStyle)
}

func (m model) renderReasoningBlock(text string) string {
	return m.renderCard(renderMarkdown(text, m.reasoningMarkdownRender), m.reasoningCardStyle)
}

func (m model) renderErrorBlock(text string) string {
	return m.renderCard(m.errorTextStyle.Render(strings.TrimSpace(text)), m.errorCardStyle)
}

func (m model) renderCard(content string, style lipgloss.Style) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	if m.vp.Width > 0 {
		style = style.Width(m.vp.Width)
	}
	return style.Render(content)
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

func (m model) renderToolWidget(b block) (string, bool) {
	width := m.vp.Width
	if width <= 0 {
		width = 80
	}
	boxWidth := width
	if boxWidth > 2 {
		boxWidth = width - 2
	}
	frameW, _ := m.toolBoxStyle.GetFrameSize()
	contentWidth := boxWidth - frameW
	if contentWidth < 1 {
		contentWidth = 1
	}

	toolName := ""
	callID := ""
	if b.Meta != nil {
		toolName = b.Meta["tool"]
		callID = b.Meta["call_id"]
	}
	if toolName == "" {
		toolName = "tool"
	}

	args := formatToolArgs(strings.TrimSpace(b.ToolArgs))
	result := strings.TrimSpace(b.ToolResult)
	expandable := false

	state := b.ToolState
	if (b.ToolResultReceived || result != "") && state == toolExecutionPending {
		state = toolExecutionDone
	}

	if state == toolExecutionPending && !b.ToolResultReceived && result == "" {
		result = m.toolPendingStyle.Render(m.toolSpinnerLabel())
	} else if result == "" {
		result = m.toolMetaStyle.Render("(empty output)")
	} else if !b.ToolResultExpanded {
		preview, truncated := truncateLines(result, toolResultPreviewLines)
		result = preview
		expandable = truncated
	}

	cardStyle := m.toolBoxStyle
	switch state {
	case toolExecutionDone:
		cardStyle = cardStyle.Copy().BorderForeground(lipgloss.Color("#3E5A45"))
	case toolExecutionError:
		cardStyle = cardStyle.Copy().BorderForeground(lipgloss.Color("#9E4D4D"))
	default:
		cardStyle = cardStyle.Copy().BorderForeground(lipgloss.Color("#446189"))
	}

	metaLine := m.toolNameStyle.Render(toolName)
	if callID != "" {
		metaLine += " " + m.toolMetaStyle.Render(callID)
	}
	metaLine = lipgloss.NewStyle().Width(contentWidth).Render(metaLine)
	requestBody := m.toolRequestStyle.Width(contentWidth).Render(args)
	responseBody := m.toolResponseStyle.Width(contentWidth).Render(result)
	divider := m.toolDividerStyle.Render(strings.Repeat("-", contentWidth))

	bodyLines := []string{
		metaLine,
		requestBody,
		divider,
		responseBody,
	}
	if expandable {
		bodyLines = append(bodyLines, m.toolExpandStyle.Render(toolResultExpandLabel))
	}
	body := lipgloss.NewStyle().Width(contentWidth).Render(strings.Join(bodyLines, "\n"))

	return cardStyle.Width(boxWidth).Render(body), expandable
}

func truncateLines(value string, maxLines int) (string, bool) {
	if maxLines < 1 {
		return "", strings.TrimSpace(value) != ""
	}
	if value == "" {
		return "", false
	}
	lines := strings.Split(value, "\n")
	if len(lines) <= maxLines {
		return value, false
	}
	return strings.Join(lines[:maxLines], "\n"), true
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

func formatToolArgs(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "{}"
	}
	if !json.Valid([]byte(value)) {
		return compactWhitespace(value)
	}

	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(value), "", "  "); err != nil {
		return compactWhitespace(value)
	}
	out := strings.TrimSpace(buf.String())
	if out == "" {
		return "{}"
	}
	return out
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
	assistantFrameW, _ := m.assistantCardStyle.GetFrameSize()
	reasoningFrameW, _ := m.reasoningCardStyle.GetFrameSize()
	frameW := assistantFrameW
	if reasoningFrameW > frameW {
		frameW = reasoningFrameW
	}

	width := m.vp.Width - frameW
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
