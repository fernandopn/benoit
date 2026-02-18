package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
	"github.com/fernandopn/benoid/providers"
	"golang.org/x/term"
)

type blockKind int

const (
	blockSystem blockKind = iota
	blockUser
	blockAssistant
	blockReasoning
	blockToolCall
	blockToolResult
	blockToolWidget
	blockContext
	blockError
)

var isTerminalAvailable = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

const (
	USER_BACKGROUND_COLOR = "#1C1C1C"
	USER_FOREGROUND_COLOR = "#E6EDF3"
)

type block struct {
	Kind       blockKind
	Text       string
	Meta       map[string]string
	ToolArgs   string
	ToolResult string
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

type toolSpinnerTick struct{}

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

	streamCh       <-chan providers.Msg
	streamCancel   context.CancelFunc
	streamSeq      int
	activeSeq      int
	toolBlockIndex map[string]int

	headerStyle    lipgloss.Style
	subHeaderStyle lipgloss.Style
	bodyStyle      lipgloss.Style
	inputBoxStyle  lipgloss.Style
	inputBgStyle   lipgloss.Style

	userTextStyle     lipgloss.Style
	toolLabelStyle    lipgloss.Style
	contextLabelStyle lipgloss.Style
	errorLabelStyle   lipgloss.Style
	toolBoxStyle      lipgloss.Style
	toolKeyStyle      lipgloss.Style
	toolBodyStyle     lipgloss.Style
	toolNameStyle     lipgloss.Style

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

	toolSpinnerFrames []string
	toolSpinnerIndex  int
	toolSpinnerActive bool
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

var runSimpleUI = func(provider providers.Provider, timeout time.Duration) {
	RunSimple(provider, timeout)
}

var runBubbleTeaUI = RunBubbleTea

func Run(ctx context.Context, provider providers.Provider, timeout time.Duration, useSimple bool) error {
	if shouldUseSimpleUI(useSimple) {
		runSimpleUI(provider, timeout)
		return nil
	}
	return runBubbleTeaUI(ctx, provider, timeout)
}

func shouldUseSimpleUI(forceSimple bool) bool {
	return forceSimple || !isTerminalAvailable()
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

	toolBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3A4452"))
	toolKey := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8FB3FF")).
		Bold(true)
	toolBody := lipgloss.NewStyle().
		Foreground(muted)
	toolName := toolKey.Copy().
		Bold(false).
		Italic(true)

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
		toolBoxStyle:  toolBox,
		toolKeyStyle:  toolKey,
		toolBodyStyle: toolBody,
		toolNameStyle: toolName,
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
		toolSpinnerFrames:      []string{"|", "/", "-", "\\"},
	}
	m.updateMarkdownRenderers()
	m.toolBlockIndex = make(map[string]int)
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
			if m.streaming {
				return m, batchCmds(cmds)
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
			cmds = append(cmds, m.syncToolSpinner())
			return m, batchCmds(cmds)
		}
		cmds = append(cmds, m.syncToolSpinner())
		return m, tea.Batch(append(cmds, readStreamChunk(m.streamCh, 25*time.Millisecond, 64, msg.Seq))...)
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
		m.vp.SetContent(m.renderTranscript())
		if wasAtBottom {
			m.vp.GotoBottom()
		}
		cmds = append(cmds, m.nextToolSpinnerTick())
		return m, batchCmds(cmds)
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
