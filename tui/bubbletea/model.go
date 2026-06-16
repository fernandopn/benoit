package bubbletea

import (
	"context"

	"github.com/charmbracelet/bubbles/table"
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
	cmds  table.Model

	commandSuggestions      []commandSuggestion
	commandSuggestionsShown bool
	commandCompletionPrefix string

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

	userTextStyle      lipgloss.Style
	assistantCardStyle lipgloss.Style
	reasoningCardStyle lipgloss.Style
	errorCardStyle     lipgloss.Style
	toolBoxStyle       lipgloss.Style
	toolBodyStyle      lipgloss.Style
	toolNameStyle      lipgloss.Style
	toolMetaStyle      lipgloss.Style
	toolRequestStyle   lipgloss.Style
	toolResponseStyle  lipgloss.Style
	toolDividerStyle   lipgloss.Style
	toolExpandStyle    lipgloss.Style
	toolPendingStyle   lipgloss.Style

	systemTextStyle         lipgloss.Style
	contextTextStyle        lipgloss.Style
	errorTextStyle          lipgloss.Style
	assistantMarkdownStyle  glamouransi.StyleConfig
	reasoningMarkdownStyle  glamouransi.StyleConfig
	assistantMarkdownRender *glamour.TermRenderer
	reasoningMarkdownRender *glamour.TermRenderer
	markdownWidth           int

	contextLeftPercent float64
	contextLeftKnown   bool
	contextTokensUsed  string
	contextTokensTotal string

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

	cmdTableStyles := table.DefaultStyles()
	cmdTableStyles.Header = cmdTableStyles.Header.Copy().
		Foreground(lipgloss.Color("#9FB4CF")).
		Bold(true)
	cmdTableStyles.Cell = cmdTableStyles.Cell.Copy().
		Foreground(lipgloss.Color("#C5D1E0"))
	cmdTableStyles.Selected = cmdTableStyles.Selected.Copy().
		Foreground(lipgloss.Color(userForegroundColor)).
		Background(lipgloss.Color("#2A3444")).
		Bold(true)
	cmdTable := table.New(
		table.WithColumns([]table.Column{
			{Title: "Command", Width: 14},
			{Title: "Description", Width: 24},
		}),
		table.WithRows(nil),
		table.WithHeight(1),
		table.WithFocused(true),
		table.WithStyles(cmdTableStyles),
	)
	cmdTable.Focus()

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
	assistantCard := lipgloss.NewStyle().
		Padding(0, 1).
		BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#364150"))
	reasoningCard := lipgloss.NewStyle().
		Padding(0, 1).
		BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#2C3440"))
	errorCard := lipgloss.NewStyle().
		Padding(0, 1).
		BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(warn)

	toolBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3A4452"))
	toolBody := lipgloss.NewStyle().
		Foreground(muted)
	toolName := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A7BDD9")).
		Bold(false).
		Italic(true)
	toolMeta := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7F8FA6"))
	toolRequest := lipgloss.NewStyle().
		Background(lipgloss.Color("#1E2A3A")).
		Foreground(lipgloss.Color("#C8D8F0")).
		Padding(0, 1)
	toolResponse := lipgloss.NewStyle().
		Background(lipgloss.Color("#141A23")).
		Foreground(lipgloss.Color("#B7C3D3")).
		Padding(0, 1)
	toolDivider := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#344153"))
	toolExpand := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8FB3FF")).
		Underline(true)
	toolPending := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8FB3FF")).
		Italic(true)

	assistantMarkdownStyle := assistantMarkdownStyleConfig()
	reasoningMarkdownStyle := reasoningMarkdownStyleConfig(muted)

	blocks := make([]block, 0, 2)
	if cfg.WelcomeText != "" {
		blocks = append(blocks, block{Kind: blockSystem, Text: cfg.WelcomeText})
	}
	if toolsText := formatEnabledTools(cfg.ToolNames); toolsText != "" {
		blocks = append(blocks, block{Kind: blockSystem, Text: toolsText})
	}

	m := model{
		ctx:            ctx,
		providerName:   cfg.ProviderName,
		helpText:       cfg.HelpText,
		startStream:    cfg.StartStream,
		vp:             vp,
		input:          ta,
		cmds:           cmdTable,
		blocks:         blocks,
		headerStyle:    header,
		subHeaderStyle: subHeader,
		bodyStyle:      body,
		inputBoxStyle:  inputBox,
		inputBgStyle:   inputBg,
		userTextStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(userForegroundColor)).
			Background(inputBgColor),
		assistantCardStyle: assistantCard,
		reasoningCardStyle: reasoningCard,
		errorCardStyle:     errorCard,
		toolBoxStyle:       toolBox,
		toolBodyStyle:      toolBody,
		toolNameStyle:      toolName,
		toolMetaStyle:      toolMeta,
		toolRequestStyle:   toolRequest,
		toolResponseStyle:  toolResponse,
		toolDividerStyle:   toolDivider,
		toolExpandStyle:    toolExpand,
		toolPendingStyle:   toolPending,
		systemTextStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7F8DA3")).
			Italic(true),
		contextTextStyle: lipgloss.NewStyle().
			Foreground(muted),
		errorTextStyle: lipgloss.NewStyle().
			Foreground(warn),
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
