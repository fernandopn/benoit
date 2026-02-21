package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fernandopn/benoit/providers"
)

type simpleProvider struct{}

func (p *simpleProvider) Chat(_ context.Context, _ string) <-chan providers.Msg {
	out := make(chan providers.Msg)
	close(out)
	return out
}

func (p *simpleProvider) PerformCompression(ctx context.Context, sessionID string, compressor providers.Compressor) (string, error) {
	_ = ctx
	_ = sessionID
	_ = compressor
	return "", errors.New("not implemented")
}

func (p *simpleProvider) ListModels(_ context.Context) ([]string, error) {
	return nil, nil
}

func (p *simpleProvider) Name() string {
	return "simple-provider"
}

func TestShouldUseSimpleUI(t *testing.T) {
	orig := isTerminalAvailable
	defer func() {
		isTerminalAvailable = orig
	}()

	isTerminalAvailable = func() bool { return true }
	if shouldUseSimpleUI(false) {
		t.Fatalf("expected non-simple mode when terminal is available")
	}
	if !shouldUseSimpleUI(true) {
		t.Fatalf("expected simple mode when forced")
	}

	isTerminalAvailable = func() bool { return false }
	if !shouldUseSimpleUI(false) {
		t.Fatalf("expected simple mode when terminal is not available")
	}
	if !shouldUseSimpleUI(true) {
		t.Fatalf("expected simple mode when forced even without terminal")
	}
}

func TestRun(t *testing.T) {
	origTerminalCheck := isTerminalAvailable
	origSimpleRunner := runSimpleUI
	origBubbleRunner := runBubbleTeaUI
	defer func() {
		isTerminalAvailable = origTerminalCheck
		runSimpleUI = origSimpleRunner
		runBubbleTeaUI = origBubbleRunner
	}()

	provider := &simpleProvider{}

	t.Run("forces simple mode when requested", func(t *testing.T) {
		isTerminalAvailable = func() bool { return true }
		simpleCalled := false
		runSimpleUI = func(_ providers.Provider, _ time.Duration) {
			simpleCalled = true
		}
		runBubbleTeaUI = func(_ context.Context, _ providers.Provider, _ time.Duration) error {
			t.Fatal("bubble tea should not run when simple mode requested")
			return nil
		}

		err := Run(context.Background(), provider, time.Second, true)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !simpleCalled {
			t.Fatal("expected simple UI runner to be called")
		}
	})

	t.Run("uses bubbletea when terminal available and not forced", func(t *testing.T) {
		isTerminalAvailable = func() bool { return true }
		simpleCalled := false
		runSimpleUI = func(_ providers.Provider, _ time.Duration) {
			simpleCalled = true
		}

		expectedErr := errors.New("bubble failure")
		runBubbleTeaUI = func(_ context.Context, _ providers.Provider, _ time.Duration) error {
			return expectedErr
		}

		err := Run(context.Background(), provider, time.Second, false)
		if err != expectedErr {
			t.Fatalf("expected bubble tea error, got %v", err)
		}
		if simpleCalled {
			t.Fatal("expected bubble UI runner to be used")
		}
	})

	t.Run("falls back to simple when no terminal", func(t *testing.T) {
		isTerminalAvailable = func() bool { return false }
		simpleCalled := false
		runSimpleUI = func(_ providers.Provider, _ time.Duration) {
			simpleCalled = true
		}
		runBubbleTeaUI = func(_ context.Context, _ providers.Provider, _ time.Duration) error {
			t.Fatal("bubble tea should not run when terminal unavailable")
			return nil
		}

		err := Run(context.Background(), provider, time.Second, false)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !simpleCalled {
			t.Fatal("expected simple UI runner to be called")
		}
	})
}
