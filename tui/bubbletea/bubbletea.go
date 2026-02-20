package bubbletea

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"
)

func NewModel(ctx context.Context, cfg Config) (tea.Model, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return newModel(ctx, cfg), nil
}

func NewProgram(ctx context.Context, cfg Config, opts ...tea.ProgramOption) (*tea.Program, error) {
	m, err := NewModel(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return tea.NewProgram(m, opts...), nil
}

func Run(ctx context.Context, cfg Config, opts ...tea.ProgramOption) error {
	prog, err := NewProgram(ctx, cfg, opts...)
	if err != nil {
		return err
	}
	_, err = prog.Run()
	return err
}
