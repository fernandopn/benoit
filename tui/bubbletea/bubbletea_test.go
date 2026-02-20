package bubbletea

import (
	"context"
	"testing"

	"github.com/fernandopn/benoit/providers"
)

func TestNewModelValidation(t *testing.T) {
	t.Run("requires context", func(t *testing.T) {
		_, err := NewModel(nil, Config{})
		if err == nil {
			t.Fatalf("expected NewModel to fail with nil context")
		}
	})

	t.Run("requires stream callback", func(t *testing.T) {
		_, err := NewModel(context.Background(), Config{})
		if err == nil {
			t.Fatalf("expected NewModel to fail with nil stream callback")
		}
	})

	t.Run("accepts explicit config", func(t *testing.T) {
		_, err := NewModel(context.Background(), Config{
			ProviderName: "simple-provider",
			WelcomeText:  DefaultWelcomeText,
			HelpText:     DefaultHelpText,
			StartStream: func(context.Context, string) (<-chan providers.Msg, context.CancelFunc, error) {
				out := make(chan providers.Msg)
				close(out)
				return out, func() {}, nil
			},
		})
		if err != nil {
			t.Fatalf("expected NewModel to succeed, got %v", err)
		}
	})
}
