package main

import (
	"strings"
	"testing"

	"github.com/fernandopn/benoit/tools"
)

func TestLoadCredentials(t *testing.T) {
	t.Run("openai required", func(t *testing.T) {
		t.Setenv(openAIAPIKeyEnv, "")
		_, err := loadCredentials(Config{Command: CommandTUI})
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
		_, err := loadCredentials(Config{Command: CommandChannelListener, Channel: ChannelTelegram})
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
		creds, err := loadCredentials(Config{Command: CommandTUI})
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

	t.Run("telegram loaded outside telegram mode when available", func(t *testing.T) {
		t.Setenv(openAIAPIKeyEnv, "openai-key")
		t.Setenv(telegramAPIKeyEnv, "  telegram-key  ")
		t.Setenv(matonAPIKeyEnv, "")
		creds, err := loadCredentials(Config{Command: CommandTUI})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.TelegramBotToken != "telegram-key" {
			t.Fatalf("expected telegram token to be loaded, got %q", creds.TelegramBotToken)
		}
	})

	t.Run("trimmed values", func(t *testing.T) {
		t.Setenv(openAIAPIKeyEnv, "  openai-key  ")
		t.Setenv(telegramAPIKeyEnv, "  telegram-key  ")
		t.Setenv(matonAPIKeyEnv, "  maton-key  ")
		creds, err := loadCredentials(Config{Command: CommandChannelListener, Channel: ChannelTelegram})
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

func TestParseRenderMode(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    RenderMode
		wantErr bool
	}{
		{name: "simple", raw: "simple", want: RenderSimple},
		{name: "bubbletea", raw: "bubbletea", want: RenderBubbleTea},
		{name: "trimmed", raw: " bubbletea ", want: RenderBubbleTea},
		{name: "case insensitive", raw: "SiMpLe", want: RenderSimple},
		{name: "invalid", raw: "nope", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseRenderMode(tc.raw)
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
				t.Fatalf("parseRenderMode(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseChannelMode(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    ChannelMode
		wantErr bool
	}{
		{name: "telegram", raw: "telegram", want: ChannelTelegram},
		{name: "trimmed", raw: " telegram ", want: ChannelTelegram},
		{name: "case insensitive", raw: "TeLeGrAm", want: ChannelTelegram},
		{name: "invalid", raw: "simple", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseChannelMode(tc.raw)
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
				t.Fatalf("parseChannelMode(%q) = %v, want %v", tc.raw, got, tc.want)
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

func TestLoadConfigDefaultsDBPath(t *testing.T) {
	const root = "/tmp/benoit"

	t.Run("tui", func(t *testing.T) {
		cfg, err := loadTUIConfig(root, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.DBPath != defaultDBPath {
			t.Fatalf("unexpected default db path: %q", cfg.DBPath)
		}
	})

	t.Run("channel_listener", func(t *testing.T) {
		cfg, err := loadChannelListenerConfig(root, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.DBPath != defaultDBPath {
			t.Fatalf("unexpected default db path: %q", cfg.DBPath)
		}
	})

	t.Run("list_sessions", func(t *testing.T) {
		cfg, err := loadListSessionsConfig(root, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.DBPath != defaultDBPath {
			t.Fatalf("unexpected default db path: %q", cfg.DBPath)
		}
	})
}

func TestLoadTUIConfigSessionIDFlag(t *testing.T) {
	cfg, err := loadTUIConfig("/tmp/benoit", []string{"-session-id", "session-123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SessionID != "session-123" {
		t.Fatalf("unexpected session id: %q", cfg.SessionID)
	}
}

func TestSelectedTools(t *testing.T) {
	const fsRoot = "/tmp/benoit-tools"

	t.Run("without maton", func(t *testing.T) {
		toolSet, err := selectedTools(Config{Command: CommandTUI, FSRoot: fsRoot})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		names := toolNames(toolSet)
		expected := []string{"code_interpreter", "web_search", "get_time", "list_files", "get_current_directory", "read_file"}
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
		toolSet, err := selectedTools(Config{Command: CommandTUI, FSRoot: fsRoot, Credentials: CredentialConfig{MatonAPIKey: "test-key"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		names := toolNames(toolSet)
		expected := []string{"code_interpreter", "web_search", "get_time", "list_files", "get_current_directory", "read_file", "maton_gcalendar", "maton_gmail"}
		if len(names) != len(expected) {
			t.Fatalf("unexpected tool count: %v", names)
		}
		for i, want := range expected {
			if names[i] != want {
				t.Fatalf("tool order mismatch at %d: got %q expected %q", i, names[i], want)
			}
		}
	})

	t.Run("with telegram", func(t *testing.T) {
		toolSet, err := selectedTools(Config{Command: CommandTUI, FSRoot: fsRoot, Credentials: CredentialConfig{TelegramBotToken: "telegram-key"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		names := toolNames(toolSet)
		expected := []string{"code_interpreter", "web_search", "get_time", "list_files", "get_current_directory", "read_file", "send_channel_message"}
		if len(names) != len(expected) {
			t.Fatalf("unexpected tool count: %v", names)
		}
		for i, want := range expected {
			if names[i] != want {
				t.Fatalf("tool order mismatch at %d: got %q expected %q", i, names[i], want)
			}
		}
	})

	t.Run("with telegram and maton", func(t *testing.T) {
		toolSet, err := selectedTools(Config{Command: CommandTUI, FSRoot: fsRoot, Credentials: CredentialConfig{TelegramBotToken: "telegram-key", MatonAPIKey: "test-key"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		names := toolNames(toolSet)
		expected := []string{"code_interpreter", "web_search", "get_time", "list_files", "get_current_directory", "read_file", "send_channel_message", "maton_gcalendar", "maton_gmail"}
		if len(names) != len(expected) {
			t.Fatalf("unexpected tool count: %v", names)
		}
		for i, want := range expected {
			if names[i] != want {
				t.Fatalf("tool order mismatch at %d: got %q expected %q", i, names[i], want)
			}
		}
	})

	t.Run("telegram disabled on channel_listener", func(t *testing.T) {
		toolSet, err := selectedTools(Config{Command: CommandChannelListener, FSRoot: fsRoot, Credentials: CredentialConfig{TelegramBotToken: "telegram-key"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		names := toolNames(toolSet)
		expected := []string{"code_interpreter", "web_search", "get_time"}
		if len(names) != len(expected) {
			t.Fatalf("unexpected tool count: %v", names)
		}
		for i, want := range expected {
			if names[i] != want {
				t.Fatalf("tool order mismatch at %d: got %q expected %q", i, names[i], want)
			}
		}
	})

	t.Run("tui requires fs root for file tools", func(t *testing.T) {
		_, err := selectedTools(Config{Command: CommandTUI, FSRoot: "  "})
		if err == nil {
			t.Fatal("expected fs root validation error")
		}
	})
}

func TestValidateConfigSessionID(t *testing.T) {
	cfg := Config{
		Command: CommandTUI,
		Render:  RenderSimple,
		Model:   "gpt-5.2",
		Credentials: CredentialConfig{
			OpenAIAPIKey: "key",
		},
	}

	t.Run("valid session id", func(t *testing.T) {
		cfg.SessionID = "session-1"
		if err := validateConfig(cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid session id", func(t *testing.T) {
		cfg.SessionID = "bad\nline"
		err := validateConfig(cfg)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "-session-id") {
			t.Fatalf("unexpected error: %v", err)
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
