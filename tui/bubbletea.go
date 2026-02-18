package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	glamourstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/fernandopn/benoid/providers"
)

type blockKind int

const (
	blockSystem blockKind = iota
	blockUser
	blockAssistant
	blockReasoning
	blockToolCall
	blockToolResult
	blockContext
	blockError
)

const (
	USER_BACKGROUND_COLOR = "#1C1C1C"
	USER_FOREGROUND_COLOR = "#E6EDF3"
)

type block struct {
	Kind blockKind
	Text string
	Meta map[string]string
}

type streamStartedMsg struct {
	Seq    int
	Ch     <-chan providers.Msg
	Cancel context.CancelFunc
}

type streamChunkMsg struct {
	Seq  int
	Msgs []providers.Msg
	Done bool
}

type model struct {
	ctx      context.Context
	provider providers.Provider
	timeout  time.Duration

	vp    viewport.Model
	input textarea.Model

	width  int
	height int

	blocks    []block
	streaming bool

	streamCh     <-chan providers.Msg
	streamCancel context.CancelFunc
	streamSeq    int
	activeSeq    int

	headerStyle    lipgloss.Style
	subHeaderStyle lipgloss.Style
	bodyStyle      lipgloss.Style
	inputBoxStyle  lipgloss.Style
	inputBgStyle   lipgloss.Style

	userTextStyle     lipgloss.Style
	toolLabelStyle    lipgloss.Style
	contextLabelStyle lipgloss.Style
	errorLabelStyle   lipgloss.Style

	systemTextStyle         lipgloss.Style
	contextTextStyle        lipgloss.Style
	errorTextStyle          lipgloss.Style
	assistantMarkdownStyle  glamouransi.StyleConfig
	reasoningMarkdownStyle  glamouransi.StyleConfig
	assistantMarkdownRender *glamour.TermRenderer
	reasoningMarkdownRender *glamour.TermRenderer
	markdownWidth           int

	contextLeft      string
	contextLeftStyle lipgloss.Style
}

func RunBubbleTea(ctx context.Context, provider providers.Provider, timeout time.Duration) error {
	m := newModel(ctx, provider, timeout)
	prog := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := prog.Run()
	return err
}

func newModel(ctx context.Context, provider providers.Provider, timeout time.Duration) model {
	ta := textarea.New()
	ta.Placeholder = "Send a message... (Shift+Enter for newline)"
	ta.Focus()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	ta.CharLimit = 10000
	ta.MaxHeight = 4
	ta.KeyMap.InsertNewline.SetEnabled(false)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	inputBgColor := lipgloss.Color(USER_BACKGROUND_COLOR)
	placeholderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8FB3FF")).
		Background(inputBgColor).
		Italic(true)
	ta.FocusedStyle.Placeholder = placeholderStyle
	ta.BlurredStyle.Placeholder = placeholderStyle
	ta.FocusedStyle.Text = lipgloss.NewStyle().
		Foreground(lipgloss.Color(USER_FOREGROUND_COLOR)).
		Background(inputBgColor)
	ta.BlurredStyle.Text = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#B7C0C9")).
		Background(inputBgColor)
	bgBase := lipgloss.NewStyle().Background(inputBgColor)
	ta.FocusedStyle.Base = bgBase
	ta.BlurredStyle.Base = bgBase
	ta.FocusedStyle.CursorLine = bgBase
	ta.BlurredStyle.CursorLine = bgBase
	ta.FocusedStyle.Prompt = bgBase
	ta.BlurredStyle.Prompt = bgBase
	ta.FocusedStyle.EndOfBuffer = bgBase
	ta.BlurredStyle.EndOfBuffer = bgBase

	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true

	muted := lipgloss.Color("#95A3B8")
	strong := lipgloss.Color("231")
	warn := lipgloss.Color("203")

	header := lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(strong)
	subHeader := lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(muted)

	body := lipgloss.NewStyle().
		Padding(0, 1)

	inputBox := lipgloss.NewStyle()

	inputBg := lipgloss.NewStyle().
		Background(inputBgColor)

	assistantMarkdownStyle := assistantMarkdownStyleConfig()
	reasoningMarkdownStyle := reasoningMarkdownStyleConfig(muted)

	m := model{
		ctx:      ctx,
		provider: provider,
		timeout:  timeout,
		vp:       vp,
		input:    ta,
		blocks: []block{
			{Kind: blockSystem, Text: "Welcome. Type a prompt and press Enter."},
		},
		headerStyle:    header,
		subHeaderStyle: subHeader,
		bodyStyle:      body,
		inputBoxStyle:  inputBox,
		inputBgStyle:   inputBg,
		userTextStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(USER_FOREGROUND_COLOR)).
			Background(inputBgColor),
		toolLabelStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CB6FF")).
			Bold(true),
		contextLabelStyle: lipgloss.NewStyle().
			Foreground(muted).
			Bold(true),
		errorLabelStyle: lipgloss.NewStyle().
			Foreground(warn).
			Bold(true),
		systemTextStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7F8DA3")).
			Italic(true),
		contextTextStyle: lipgloss.NewStyle().
			Foreground(muted),
		errorTextStyle: lipgloss.NewStyle().
			Foreground(warn),
		contextLeftStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8FB3FF")).
			Bold(true),
		assistantMarkdownStyle: assistantMarkdownStyle,
		reasoningMarkdownStyle: reasoningMarkdownStyle,
	}
	m.updateMarkdownRenderers()
	return m
}

func (m model) Init() tea.Cmd {
	return textarea.Blink
}

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
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
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
			m.blocks = append(m.blocks, block{Kind: blockUser, Text: prompt})
			m.input.Reset()

			m.vp.SetContent(m.renderTranscript())
			m.vp.GotoBottom()

			m.streaming = true
			m.streamSeq++
			m.activeSeq = m.streamSeq
			return m, tea.Batch(append(cmds, startStream(m.ctx, m.provider, prompt, m.timeout, m.activeSeq))...)
		}

	case streamStartedMsg:
		if msg.Seq != m.activeSeq {
			if msg.Cancel != nil {
				msg.Cancel()
			}
			return m, batchCmds(cmds)
		}
		m.streamCh = msg.Ch
		m.streamCancel = msg.Cancel
		return m, tea.Batch(append(cmds, readStreamChunk(m.streamCh, 25*time.Millisecond, 64, msg.Seq))...)

	case streamChunkMsg:
		if msg.Seq != m.activeSeq {
			return m, batchCmds(cmds)
		}

		wasAtBottom := m.vp.AtBottom()
		if len(msg.Msgs) > 0 {
			m.applyStreamMessages(msg.Msgs)
			m.vp.SetContent(m.renderTranscript())
		}
		if wasAtBottom {
			m.vp.GotoBottom()
		}

		if msg.Done {
			m.streaming = false
			m.cancelStreamIfAny()
			return m, batchCmds(cmds)
		}
		return m, tea.Batch(append(cmds, readStreamChunk(m.streamCh, 25*time.Millisecond, 64, msg.Seq))...)
	}

	return m, batchCmds(cmds)
}

func (m model) View() string {
	width := max(0, m.width)
	header := m.headerStyle.Width(width).Render(m.headerLine())
	subHeader := m.subHeaderStyle.Width(width).Render("Enter to send | /exit to quit | PgUp/PgDn or mouse wheel to scroll")
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

func (m model) headerLine() string {
	left := fmt.Sprintf("Benoid · %s", m.provider.Name())
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

func startStream(ctx context.Context, provider providers.Provider, prompt string, timeout time.Duration, seq int) tea.Cmd {
	return func() tea.Msg {
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		ch := provider.Chat(reqCtx, prompt)
		return streamStartedMsg{Seq: seq, Ch: ch, Cancel: cancel}
	}
}

func readStreamChunk(ch <-chan providers.Msg, maxWait time.Duration, maxMsgs int, seq int) tea.Cmd {
	return func() tea.Msg {
		var items []providers.Msg

		first, ok := <-ch
		if !ok {
			return streamChunkMsg{Seq: seq, Done: true}
		}
		items = append(items, first)

		timer := time.NewTimer(maxWait)
		defer timer.Stop()

		for len(items) < maxMsgs {
			select {
			case msg, ok := <-ch:
				if !ok {
					return streamChunkMsg{Seq: seq, Msgs: items, Done: true}
				}
				items = append(items, msg)
			case <-timer.C:
				return streamChunkMsg{Seq: seq, Msgs: items, Done: false}
			}
		}

		return streamChunkMsg{Seq: seq, Msgs: items, Done: false}
	}
}

func (m *model) applyStreamMessages(msgs []providers.Msg) {
	for _, msg := range msgs {
		switch msg.Type {
		case providers.MsgTypeChat:
			m.appendToBlock(blockAssistant, msg.Value, nil)
		case providers.MsgTypeReasoningSummary:
			m.appendToBlock(blockReasoning, msg.Value, nil)
		case providers.MsgTypeToolCall:
			m.appendToBlock(blockToolCall, msg.Value, msg.Metadata)
		case providers.MsgTypeToolResult:
			m.appendToBlock(blockToolResult, msg.Value, msg.Metadata)
		case providers.MsgTypeContextUsage:
			m.updateContextUsage(msg.Value, msg.Metadata)
		case providers.MsgTypeError:
			m.appendBlock(blockError, msg.Value, msg.Metadata)
			m.streaming = false
			m.cancelStreamIfAny()
		}
	}
}

func (m *model) appendToBlock(kind blockKind, text string, meta map[string]string) {
	if kind == blockContext || kind == blockError {
		m.appendBlock(kind, text, meta)
		return
	}

	if len(m.blocks) > 0 {
		last := &m.blocks[len(m.blocks)-1]
		if last.Kind == kind && compatibleMeta(kind, last.Meta, meta) {
			last.Text += text
			return
		}
	}
	m.appendBlock(kind, text, meta)
}

func (m *model) appendBlock(kind blockKind, text string, meta map[string]string) {
	m.blocks = append(m.blocks, block{
		Kind: kind,
		Text: text,
		Meta: cloneMeta(meta),
	})
}

func compatibleMeta(kind blockKind, current, incoming map[string]string) bool {
	if kind != blockToolCall && kind != blockToolResult {
		return true
	}
	if current == nil || incoming == nil {
		return false
	}
	if current["call_id"] == "" || incoming["call_id"] == "" {
		return false
	}
	return current["call_id"] == incoming["call_id"]
}

func cloneMeta(meta map[string]string) map[string]string {
	if len(meta) == 0 {
		return nil
	}
	clone := make(map[string]string, len(meta))
	for k, v := range meta {
		clone[k] = v
	}
	return clone
}

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

func (m *model) cancelStreamIfAny() {
	if m.streamCancel != nil {
		m.streamCancel()
	}
	m.streamCancel = nil
	m.streamCh = nil
}

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
