package bubbletea

import (
	"context"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
	"github.com/fernandopn/benoit/providers"
)

type model struct {
	ctx          context.Context
	providerName string
	helpText     string
	startStream  StreamStarter

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
	toolExpandStyle   lipgloss.Style

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
	toolExpandTargets []toolExpandTarget
}

func newModel(ctx context.Context, cfg Config) model {
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
	inputBgColor := lipgloss.Color(userBackgroundColor)
	placeholderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8FB3FF")).
		Background(inputBgColor).
		Italic(true)
	ta.FocusedStyle.Placeholder = placeholderStyle
	ta.BlurredStyle.Placeholder = placeholderStyle
	ta.FocusedStyle.Text = lipgloss.NewStyle().
		Foreground(lipgloss.Color(userForegroundColor)).
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
	toolExpand := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8FB3FF")).
		Underline(true)

	assistantMarkdownStyle := assistantMarkdownStyleConfig()
	reasoningMarkdownStyle := reasoningMarkdownStyleConfig(muted)

	blocks := make([]block, 0, 1)
	if cfg.WelcomeText != "" {
		blocks = append(blocks, block{Kind: blockSystem, Text: cfg.WelcomeText})
	}

	m := model{
		ctx:            ctx,
		providerName:   cfg.ProviderName,
		helpText:       cfg.HelpText,
		startStream:    cfg.StartStream,
		vp:             vp,
		input:          ta,
		blocks:         blocks,
		headerStyle:    header,
		subHeaderStyle: subHeader,
		bodyStyle:      body,
		inputBoxStyle:  inputBox,
		inputBgStyle:   inputBg,
		userTextStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(userForegroundColor)).
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
		toolBoxStyle:    toolBox,
		toolKeyStyle:    toolKey,
		toolBodyStyle:   toolBody,
		toolNameStyle:   toolName,
		toolExpandStyle: toolExpand,
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
