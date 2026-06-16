package tui

import (
	"context"
	"errors"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fernandopn/benoit/providers"
	"github.com/fernandopn/benoit/session"
	bubbleteaui "github.com/fernandopn/benoit/tui/bubbletea"
	"golang.org/x/term"
)

var isTerminalAvailable = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func RunBubbleTea(ctx context.Context, provider providers.Provider, timeout time.Duration, sessionID string, toolNames []string) error {
	return runBubbleTeaUI(ctx, provider, timeout, sessionID, toolNames)
}

var runSimpleUI = func(ctx context.Context, provider providers.Provider, timeout time.Duration, sessionID string, toolNames []string) {
	RunSimple(ctx, provider, timeout, sessionID, toolNames)
}

var runBubbleTeaUI = runBubbleTea

func runBubbleTea(ctx context.Context, provider providers.Provider, timeout time.Duration, sessionID string, toolNames []string) error {
	if provider == nil {
		return errors.New("provider is required")
	}
	sessionID = session.ResolveTUISessionID(sessionID)
	start := streamStartForProvider(provider, sessionID)

	cfg := bubbleteaui.Config{
		ProviderName: provider.Name(),
		WelcomeText:  bubbleteaui.DefaultWelcomeText,
		HelpText:     bubbleteaui.DefaultHelpText,
		ToolNames:    toolNames,
		StartStream: func(reqCtx context.Context, prompt string) (<-chan providers.Msg, context.CancelFunc, error) {
			streamCtx := reqCtx
			cancel := func() {}
			if timeout > 0 {
				streamCtx, cancel = context.WithTimeout(reqCtx, timeout)
			}
			stream := start(streamCtx, prompt)
			if stream == nil {
				cancel()
				return nil, func() {}, errors.New("provider stream is not configured")
			}
			return stream, cancel, nil
		},
	}

	return bubbleteaui.Run(ctx, cfg, tea.WithAltScreen(), tea.WithMouseCellMotion())
}

func Run(ctx context.Context, provider providers.Provider, timeout time.Duration, useSimple bool, sessionID string, toolNames []string) error {
	if shouldUseSimpleUI(useSimple) {
		runSimpleUI(ctx, provider, timeout, sessionID, toolNames)
		return nil
	}
	return runBubbleTeaUI(ctx, provider, timeout, sessionID, toolNames)
}

func shouldUseSimpleUI(forceSimple bool) bool {
	return forceSimple || !isTerminalAvailable()
}
