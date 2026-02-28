package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fernandopn/benoit/tools"
)

func TestLoadCredentials(t *testing.T) {
	t.Run("openai required", func(t *testing.T) {
		t.Setenv(openAIAPIKeyEnv, "")
		_, err := loadCredentials(Config{Command: CommandTUI}, nil)
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
		_, err := loadCredentials(Config{Command: CommandChannelListener, Channel: ChannelTelegram}, nil)
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
		creds, err := loadCredentials(Config{Command: CommandTUI}, nil)
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
		creds, err := loadCredentials(Config{Command: CommandTUI}, nil)
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
		creds, err := loadCredentials(Config{Command: CommandChannelListener, Channel: ChannelTelegram}, nil)
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

	t.Run("env file values override process env", func(t *testing.T) {
		t.Setenv(openAIAPIKeyEnv, "openai-from-env")
		t.Setenv(telegramAPIKeyEnv, "telegram-from-env")
		t.Setenv(matonAPIKeyEnv, "maton-from-env")

		envFileValues := map[string]string{
			openAIAPIKeyEnv:   "openai-from-file",
			telegramAPIKeyEnv: "telegram-from-file",
			matonAPIKeyEnv:    "maton-from-file",
		}

		creds, err := loadCredentials(Config{Command: CommandChannelListener, Channel: ChannelTelegram}, envFileValues)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.OpenAIAPIKey != "openai-from-file" {
			t.Fatalf("unexpected OpenAI key: %q", creds.OpenAIAPIKey)
		}
		if creds.TelegramBotToken != "telegram-from-file" {
			t.Fatalf("unexpected telegram token: %q", creds.TelegramBotToken)
		}
		if creds.MatonAPIKey != "maton-from-file" {
			t.Fatalf("unexpected Maton key: %q", creds.MatonAPIKey)
		}
	})

	t.Run("falls back to process env when env file is missing key", func(t *testing.T) {
		t.Setenv(openAIAPIKeyEnv, "openai-from-env")
		t.Setenv(telegramAPIKeyEnv, "")
		t.Setenv(matonAPIKeyEnv, "")

		envFileValues := map[string]string{}
		creds, err := loadCredentials(Config{Command: CommandTUI}, envFileValues)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.OpenAIAPIKey != "openai-from-env" {
			t.Fatalf("unexpected OpenAI key: %q", creds.OpenAIAPIKey)
		}
	})
}

func TestLoadDotEnvIfExists(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		values, err := loadDotEnvIfExists(filepath.Join(t.TempDir(), "missing.env"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(values) != 0 {
			t.Fatalf("expected empty values, got %v", values)
		}
	})

	t.Run("loads values", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".env")
		content := strings.Join([]string{
			"# comment",
			"OPENAI_API_KEY=openai-from-file",
			"TELEGRAM_API_KEY=\"telegram from file\"",
			"MATON_API_KEY='maton-from-file'",
			"export EXTRA=value",
			"TRIMMED=value # trailing comment",
			"",
		}, "\n")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write env file: %v", err)
		}

		values, err := loadDotEnvIfExists(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if values[openAIAPIKeyEnv] != "openai-from-file" {
			t.Fatalf("unexpected OpenAI value: %q", values[openAIAPIKeyEnv])
		}
		if values[telegramAPIKeyEnv] != "telegram from file" {
			t.Fatalf("unexpected telegram value: %q", values[telegramAPIKeyEnv])
		}
		if values[matonAPIKeyEnv] != "maton-from-file" {
			t.Fatalf("unexpected Maton value: %q", values[matonAPIKeyEnv])
		}
		if values["EXTRA"] != "value" {
			t.Fatalf("unexpected EXTRA value: %q", values["EXTRA"])
		}
		if values["TRIMMED"] != "value" {
			t.Fatalf("unexpected TRIMMED value: %q", values["TRIMMED"])
		}
	})

	t.Run("parse error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(path, []byte("BROKEN_LINE"), 0o600); err != nil {
			t.Fatalf("write env file: %v", err)
		}
		_, err := loadDotEnvIfExists(path)
		if err == nil {
			t.Fatal("expected parse error")
		}
		if !strings.Contains(err.Error(), "line 1") {
			t.Fatalf("unexpected error: %v", err)
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
		if cfg.EnvFilePath != defaultEnvFilePath {
			t.Fatalf("unexpected default env file path: %q", cfg.EnvFilePath)
		}
	})

	t.Run("ssh", func(t *testing.T) {
		cfg, err := loadSSHConfig(root, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.DBPath != defaultDBPath {
			t.Fatalf("unexpected default db path: %q", cfg.DBPath)
		}
		if cfg.EnvFilePath != defaultEnvFilePath {
			t.Fatalf("unexpected default env file path: %q", cfg.EnvFilePath)
		}
		if cfg.SSHPort != defaultSSHPort {
			t.Fatalf("unexpected default ssh port: %d", cfg.SSHPort)
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
		if cfg.EnvFilePath != defaultEnvFilePath {
			t.Fatalf("unexpected default env file path: %q", cfg.EnvFilePath)
		}
		if len(cfg.TelegramAllowedUserIDs) != 0 {
			t.Fatalf("expected empty default telegram allowlist, got %v", cfg.TelegramAllowedUserIDs)
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
		if cfg.EnvFilePath != defaultEnvFilePath {
			t.Fatalf("unexpected default env file path: %q", cfg.EnvFilePath)
		}
	})
}

func TestLoadSSHConfigMatchesTUISharedFlags(t *testing.T) {
	args := []string{
		"-session-id", "session-123",
		"-model", "gpt-5.2-codex",
		"-timeout", "30s",
		"-fs-root", "/tmp/custom",
		"-db-path", "custom.sqlite",
		"-bypass-compression-barrier",
	}
	tuiCfg, err := loadTUIConfig("/tmp/benoit", args)
	if err != nil {
		t.Fatalf("unexpected tui error: %v", err)
	}
	sshCfg, err := loadSSHConfig("/tmp/benoit", args)
	if err != nil {
		t.Fatalf("unexpected ssh error: %v", err)
	}

	if sshCfg.Render != RenderBubbleTea {
		t.Fatalf("unexpected ssh render mode: %q", sshCfg.Render)
	}
	if tuiCfg.SessionID != sshCfg.SessionID {
		t.Fatalf("session id mismatch: tui=%q ssh=%q", tuiCfg.SessionID, sshCfg.SessionID)
	}
	if tuiCfg.Model != sshCfg.Model {
		t.Fatalf("model mismatch: tui=%q ssh=%q", tuiCfg.Model, sshCfg.Model)
	}
	if tuiCfg.Timeout != sshCfg.Timeout {
		t.Fatalf("timeout mismatch: tui=%v ssh=%v", tuiCfg.Timeout, sshCfg.Timeout)
	}
	if tuiCfg.EnvFilePath != sshCfg.EnvFilePath {
		t.Fatalf("env file mismatch: tui=%q ssh=%q", tuiCfg.EnvFilePath, sshCfg.EnvFilePath)
	}
	if tuiCfg.FSRoot != sshCfg.FSRoot {
		t.Fatalf("fs root mismatch: tui=%q ssh=%q", tuiCfg.FSRoot, sshCfg.FSRoot)
	}
	if tuiCfg.FSRootProvided != sshCfg.FSRootProvided {
		t.Fatalf("fs root provided mismatch: tui=%v ssh=%v", tuiCfg.FSRootProvided, sshCfg.FSRootProvided)
	}
	if tuiCfg.DBPath != sshCfg.DBPath {
		t.Fatalf("db path mismatch: tui=%q ssh=%q", tuiCfg.DBPath, sshCfg.DBPath)
	}
	if tuiCfg.BypassCompressionBarrier != sshCfg.BypassCompressionBarrier {
		t.Fatalf("bypass compression mismatch: tui=%v ssh=%v", tuiCfg.BypassCompressionBarrier, sshCfg.BypassCompressionBarrier)
	}
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

func TestLoadConfigSSHCommand(t *testing.T) {
	cfg, err := loadConfig("/tmp/benoit", []string{"ssh"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Command != CommandSSH {
		t.Fatalf("unexpected command: %q", cfg.Command)
	}
	if cfg.Render != RenderBubbleTea {
		t.Fatalf("unexpected default render: %q", cfg.Render)
	}
	if cfg.SSHPort != defaultSSHPort {
		t.Fatalf("unexpected default ssh port: %d", cfg.SSHPort)
	}
}

func TestLoadSSHConfigRejectsRenderFlag(t *testing.T) {
	_, err := loadSSHConfig("/tmp/benoit", []string{"-render", "simple"})
	if err == nil {
		t.Fatal("expected unknown render flag error")
	}
}

func TestLoadSSHConfigPortFlag(t *testing.T) {
	cfg, err := loadSSHConfig("/tmp/benoit", []string{"-ssh-port", "2200"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SSHPort != 2200 {
		t.Fatalf("unexpected ssh port: %d", cfg.SSHPort)
	}
}

func TestLoadConfigEnvFileFlag(t *testing.T) {
	t.Run("tui", func(t *testing.T) {
		cfg, err := loadTUIConfig("/tmp/benoit", []string{"-env-file", "./custom.env"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.EnvFilePath != "./custom.env" {
			t.Fatalf("unexpected env file path: %q", cfg.EnvFilePath)
		}
	})

	t.Run("ssh", func(t *testing.T) {
		cfg, err := loadSSHConfig("/tmp/benoit", []string{"-env-file", "./custom.env"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.EnvFilePath != "./custom.env" {
			t.Fatalf("unexpected env file path: %q", cfg.EnvFilePath)
		}
	})

	t.Run("channel_listener", func(t *testing.T) {
		cfg, err := loadChannelListenerConfig("/tmp/benoit", []string{"-env-file", "./custom.env"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.EnvFilePath != "./custom.env" {
			t.Fatalf("unexpected env file path: %q", cfg.EnvFilePath)
		}
	})

	t.Run("list_sessions", func(t *testing.T) {
		cfg, err := loadListSessionsConfig("/tmp/benoit", []string{"-env-file", "./custom.env"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.EnvFilePath != "./custom.env" {
			t.Fatalf("unexpected env file path: %q", cfg.EnvFilePath)
		}
	})
}

func TestLoadTUIConfigFSRootFlag(t *testing.T) {
	t.Run("not provided", func(t *testing.T) {
		cfg, err := loadTUIConfig("/tmp/benoit", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.FSRootProvided {
			t.Fatal("expected FSRootProvided to be false")
		}
	})

	t.Run("provided", func(t *testing.T) {
		cfg, err := loadTUIConfig("/tmp/benoit", []string{"-fs-root", "/tmp/custom"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.FSRootProvided {
			t.Fatal("expected FSRootProvided to be true")
		}
		if cfg.FSRoot != "/tmp/custom" {
			t.Fatalf("unexpected fs root: %q", cfg.FSRoot)
		}
	})
}

func TestSelectedTools(t *testing.T) {
	const fsRoot = "/tmp/benoit-tools"

	t.Run("without fs root", func(t *testing.T) {
		toolSet, err := selectedTools(Config{Command: CommandTUI})
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

	t.Run("ssh without fs root", func(t *testing.T) {
		toolSet, err := selectedTools(Config{Command: CommandSSH})
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

	t.Run("with fs root", func(t *testing.T) {
		toolSet, err := selectedTools(Config{Command: CommandTUI, FSRoot: fsRoot, FSRootProvided: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		names := toolNames(toolSet)
		expected := []string{"code_interpreter", "web_search", "get_time", "glob", "grep", "read", "write", "apply_patch"}
		if len(names) != len(expected) {
			t.Fatalf("unexpected tool count: %v", names)
		}
		for i, want := range expected {
			if names[i] != want {
				t.Fatalf("tool order mismatch at %d: got %q expected %q", i, names[i], want)
			}
		}
	})

	t.Run("ssh with fs root", func(t *testing.T) {
		toolSet, err := selectedTools(Config{Command: CommandSSH, FSRoot: fsRoot, FSRootProvided: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		names := toolNames(toolSet)
		expected := []string{"code_interpreter", "web_search", "get_time", "glob", "grep", "read", "write", "apply_patch"}
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
		toolSet, err := selectedTools(Config{Command: CommandTUI, FSRoot: fsRoot, FSRootProvided: true, Credentials: CredentialConfig{MatonAPIKey: "test-key"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		names := toolNames(toolSet)
		expected := []string{"code_interpreter", "web_search", "get_time", "glob", "grep", "read", "write", "apply_patch", "maton_gcalendar", "maton_gmail"}
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
		toolSet, err := selectedTools(Config{Command: CommandTUI, FSRoot: fsRoot, FSRootProvided: true, Credentials: CredentialConfig{TelegramBotToken: "telegram-key"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		names := toolNames(toolSet)
		expected := []string{"code_interpreter", "web_search", "get_time", "glob", "grep", "read", "write", "apply_patch", "send_channel_message"}
		if len(names) != len(expected) {
			t.Fatalf("unexpected tool count: %v", names)
		}
		for i, want := range expected {
			if names[i] != want {
				t.Fatalf("tool order mismatch at %d: got %q expected %q", i, names[i], want)
			}
		}
	})

	t.Run("with telegram on ssh", func(t *testing.T) {
		toolSet, err := selectedTools(Config{Command: CommandSSH, FSRoot: fsRoot, FSRootProvided: true, Credentials: CredentialConfig{TelegramBotToken: "telegram-key"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		names := toolNames(toolSet)
		expected := []string{"code_interpreter", "web_search", "get_time", "glob", "grep", "read", "write", "apply_patch", "send_channel_message"}
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
		toolSet, err := selectedTools(Config{Command: CommandTUI, FSRoot: fsRoot, FSRootProvided: true, Credentials: CredentialConfig{TelegramBotToken: "telegram-key", MatonAPIKey: "test-key"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		names := toolNames(toolSet)
		expected := []string{"code_interpreter", "web_search", "get_time", "glob", "grep", "read", "write", "apply_patch", "send_channel_message", "maton_gcalendar", "maton_gmail"}
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
		toolSet, err := selectedTools(Config{Command: CommandChannelListener, FSRoot: fsRoot, FSRootProvided: true, Credentials: CredentialConfig{TelegramBotToken: "telegram-key"}})
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

	t.Run("tui requires fs root when flag is provided", func(t *testing.T) {
		_, err := selectedTools(Config{Command: CommandTUI, FSRootProvided: true, FSRoot: "  "})
		if err == nil {
			t.Fatal("expected fs root validation error")
		}
	})

	t.Run("ssh requires fs root when flag is provided", func(t *testing.T) {
		_, err := selectedTools(Config{Command: CommandSSH, FSRootProvided: true, FSRoot: "  "})
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

	t.Run("invalid fs root when provided", func(t *testing.T) {
		cfg.SessionID = "session-1"
		cfg.FSRootProvided = true
		cfg.FSRoot = "  "
		err := validateConfig(cfg)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "-fs-root") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestValidateConfigSessionIDSSH(t *testing.T) {
	cfg := Config{
		Command: CommandSSH,
		Render:  RenderBubbleTea,
		SSHPort: defaultSSHPort,
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

	t.Run("invalid ssh port", func(t *testing.T) {
		cfg.SessionID = "session-1"
		cfg.SSHPort = 0
		err := validateConfig(cfg)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "-ssh-port") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid ssh render mode", func(t *testing.T) {
		cfg.SessionID = "session-1"
		cfg.SSHPort = defaultSSHPort
		cfg.Render = RenderSimple
		err := validateConfig(cfg)
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "bubbletea") {
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
