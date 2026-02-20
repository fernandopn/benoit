package bubbletea

import (
	"context"
	"errors"

	"github.com/fernandopn/benoit/providers"
)

const (
	userBackgroundColor    = "#1C1C1C"
	userForegroundColor    = "#E6EDF3"
	toolResultPreviewLines = 4
	toolResultExpandLabel  = "[expand]"
)

const (
	DefaultWelcomeText = "Welcome. Type a prompt and press Enter."
	DefaultHelpText    = "Enter to send | /exit to quit | PgUp/PgDn or mouse wheel to scroll"
)

type StreamStarter func(ctx context.Context, prompt string) (<-chan providers.Msg, context.CancelFunc, error)

type Config struct {
	ProviderName string
	WelcomeText  string
	HelpText     string
	StartStream  StreamStarter
}

func (cfg Config) validate() error {
	if cfg.StartStream == nil {
		return errors.New("start stream callback is required")
	}
	return nil
}

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

type block struct {
	Kind               blockKind
	Text               string
	Meta               map[string]string
	ToolArgs           string
	ToolResult         string
	ToolState          toolExecutionState
	ToolResultExpanded bool
}

type toolExecutionState int

const (
	toolExecutionPending toolExecutionState = iota
	toolExecutionDone
	toolExecutionError
)

type toolExpandTarget struct {
	BlockIndex int
	Line       int
	ColStart   int
	ColEnd     int
}

type streamStartedMsg struct {
	Seq    int
	Ch     <-chan providers.Msg
	Cancel context.CancelFunc
}

type streamStartFailedMsg struct {
	Seq    int
	Err    error
	Cancel context.CancelFunc
}

type streamChunkMsg struct {
	Seq  int
	Msgs []providers.Msg
	Done bool
}

type toolSpinnerTick struct{}
