package main

import (
	"strings"
	"testing"

	"github.com/fernandopn/benoit/tools"
)

func TestLoadCredentials(t *testing.T) {
	t.Run("openai required", func(t *testing.T) {
		t.Setenv(openAIAPIKeyEnv, "")
		_, err := loadCredentials(ModeSimple)
		if err == nil {
			t.Fatal("expected missing OPENAI_API_KEY error")
		}
		if !strings.Contains(err.Error(), openAIAPIKeyEnv) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("telegram required in telegram mode", func(t *testing.T) {
		t.Setenv(openAIAPIKeyEnv, "openai-key")
		t.Setenv(telegramAPIKeyEnv, "")
		_, err := loadCredentials(ModeTelegram)
		if err == nil {
			t.Fatal("expected missing TELEGRAM_API_KEY error")
		}
		if !strings.Contains(err.Error(), telegramAPIKeyEnv) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("telegram optional outside telegram mode", func(t *testing.T) {
		t.Setenv(openAIAPIKeyEnv, "openai-key")
		t.Setenv(telegramAPIKeyEnv, "")
		t.Setenv(matonAPIKeyEnv, "")
		creds, err := loadCredentials(ModeSimple)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.OpenAIAPIKey != "openai-key" {
			t.Fatalf("unexpected OpenAI key: %q", creds.OpenAIAPIKey)
		}
		if creds.TelegramBotToken != "" {
			t.Fatalf("expected empty telegram token, got %q", creds.TelegramBotToken)
		}
		if creds.MatonAPIKey != "" {
			t.Fatalf("expected empty Maton key, got %q", creds.MatonAPIKey)
		}
	})

	t.Run("trimmed values", func(t *testing.T) {
		t.Setenv(openAIAPIKeyEnv, "  openai-key  ")
		t.Setenv(telegramAPIKeyEnv, "  telegram-key  ")
		t.Setenv(matonAPIKeyEnv, "  maton-key  ")
		creds, err := loadCredentials(ModeTelegram)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.OpenAIAPIKey != "openai-key" {
			t.Fatalf("unexpected OpenAI key: %q", creds.OpenAIAPIKey)
		}
		if creds.TelegramBotToken != "telegram-key" {
			t.Fatalf("unexpected telegram token: %q", creds.TelegramBotToken)
		}
		if creds.MatonAPIKey != "maton-key" {
			t.Fatalf("unexpected Maton key: %q", creds.MatonAPIKey)
		}
	})
}

func TestParseTUIMode(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Mode
		wantErr bool
	}{
		{name: "simple", raw: "simple", want: ModeSimple},
		{name: "bubbletea", raw: "bubbletea", want: ModeBubbleTea},
		{name: "telegram", raw: "telegram", want: ModeTelegram},
		{name: "trimmed", raw: " bubbletea ", want: ModeBubbleTea},
		{name: "case insensitive", raw: "SiMpLe", want: ModeSimple},
		{name: "invalid", raw: "nope", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTUIMode(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseTUIMode(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseTelegramAllowedUsers(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		ids, err := parseTelegramAllowedUsers("  ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ids) != 0 {
			t.Fatalf("expected no IDs, got %v", ids)
		}
	})

	t.Run("valid and deduplicated", func(t *testing.T) {
		ids, err := parseTelegramAllowedUsers("77, 99, 77, 0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ids) != 2 || ids[0] != 77 || ids[1] != 99 {
			t.Fatalf("unexpected IDs: %v", ids)
		}
	})

	t.Run("invalid id", func(t *testing.T) {
		_, err := parseTelegramAllowedUsers("77,abc")
		if err == nil {
			t.Fatal("expected parse error")
		}
	})
}

func TestSelectedTools(t *testing.T) {
	t.Run("without maton", func(t *testing.T) {
		toolSet, err := selectedTools(Config{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		names := toolNames(toolSet)
		expected := []string{"code_interpreter", "web_search"}
		if len(names) != len(expected) {
			t.Fatalf("unexpected tool count: %v", names)
		}
		for i, want := range expected {
			if names[i] != want {
				t.Fatalf("tool order mismatch at %d: got %q expected %q", i, names[i], want)
			}
		}
	})

	t.Run("with maton", func(t *testing.T) {
		toolSet, err := selectedTools(Config{Credentials: CredentialConfig{MatonAPIKey: "test-key"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		names := toolNames(toolSet)
		expected := []string{"code_interpreter", "web_search", "maton_gcalendar", "maton_gmail"}
		if len(names) != len(expected) {
			t.Fatalf("unexpected tool count: %v", names)
		}
		for i, want := range expected {
			if names[i] != want {
				t.Fatalf("tool order mismatch at %d: got %q expected %q", i, names[i], want)
			}
		}
	})
}

func toolNames(toolSet []tools.Tool) []string {
	names := make([]string, 0, len(toolSet))
	for _, tool := range toolSet {
		names = append(names, tool.Name())
	}
	return names
}
