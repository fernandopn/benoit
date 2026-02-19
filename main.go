package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fernandopn/benoit/internal/app"
	"github.com/fernandopn/benoit/providers"
	"github.com/openai/openai-go/v3/shared"
)

const (
	openAIReasoningEffort             = shared.ReasoningEffortHigh
	openAIReasoningSummary            = shared.ReasoningSummaryDetailed
	defaultTUIMode                    = string(app.ModeSimple)
	defaultTelegramPollTimeoutSeconds = 30
	defaultTelegramAllowedUsers       = "8230557735"

	openAIAPIKeyEnv   = "OPENAI_API_KEY"
	telegramAPIKeyEnv = "TELEGRAM_API_KEY"
	matonAPIKeyEnv    = "MATON_API_KEY"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	defaultRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("filesystem init error: %w", err)
	}

	cfg, err := loadConfig(defaultRoot)
	if err != nil {
		return err
	}

	creds, err := loadCredentials(cfg.Mode)
	if err != nil {
		return err
	}
	cfg.Credentials = creds
	cfg.OpenAIParams = providers.OpenAIParams{
		ReasoningEffort:  openAIReasoningEffort,
		ReasoningSummary: openAIReasoningSummary,
	}

	if err := app.Run(context.Background(), cfg); err != nil {
		return err
	}
	return nil
}

func loadConfig(defaultRoot string) (app.Config, error) {
	model := flag.String("model", "gpt-5.2", "model name")
	timeout := flag.Duration("timeout", 20*time.Minute, "request timeout (e.g. 45s, 2m)")
	fsRoot := flag.String("fs-root", defaultRoot, "filesystem root")
	dbPath := flag.String("db-path", "", "sqlite db path for chat logging")
	tuiModeFlag := flag.String("tui", defaultTUIMode, "tui mode: simple, bubbletea, or telegram")
	telegramPollTimeoutSeconds := flag.Int("telegram-poll-timeout", defaultTelegramPollTimeoutSeconds, "telegram getUpdates long poll timeout in seconds")
	telegramAllowedUsersRaw := flag.String("telegram-allowed-users", defaultTelegramAllowedUsers, "comma-separated Telegram user IDs allowed in telegram mode")
	flag.Parse()

	mode, err := parseTUIMode(*tuiModeFlag)
	if err != nil {
		return app.Config{}, fmt.Errorf("flag error: %w", err)
	}
	if *telegramPollTimeoutSeconds < 0 {
		return app.Config{}, fmt.Errorf("flag error: -telegram-poll-timeout cannot be negative")
	}
	allowedUsers, err := parseTelegramAllowedUsers(*telegramAllowedUsersRaw)
	if err != nil {
		return app.Config{}, fmt.Errorf("flag error: %w", err)
	}

	return app.Config{
		Model:                      strings.TrimSpace(*model),
		Timeout:                    *timeout,
		FSRoot:                     strings.TrimSpace(*fsRoot),
		DBPath:                     strings.TrimSpace(*dbPath),
		Mode:                       mode,
		TelegramPollTimeoutSeconds: *telegramPollTimeoutSeconds,
		TelegramAllowedUserIDs:     allowedUsers,
	}, nil
}

func loadCredentials(mode app.Mode) (app.CredentialConfig, error) {
	openAIAPIKey, err := requiredEnv(openAIAPIKeyEnv)
	if err != nil {
		return app.CredentialConfig{}, fmt.Errorf("credential error: %w", err)
	}
	telegramBotToken := ""
	if mode == app.ModeTelegram {
		telegramBotToken, err = requiredEnv(telegramAPIKeyEnv)
		if err != nil {
			return app.CredentialConfig{}, fmt.Errorf("credential error: %w", err)
		}
	}
	return app.CredentialConfig{
		OpenAIAPIKey:     openAIAPIKey,
		TelegramBotToken: telegramBotToken,
		MatonAPIKey:      strings.TrimSpace(os.Getenv(matonAPIKeyEnv)),
	}, nil
}

func requiredEnv(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is not set", name)
	}
	return value, nil
}

func parseTUIMode(raw string) (app.Mode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(app.ModeSimple):
		return app.ModeSimple, nil
	case string(app.ModeBubbleTea):
		return app.ModeBubbleTea, nil
	case string(app.ModeTelegram):
		return app.ModeTelegram, nil
	default:
		return app.ModeSimple, fmt.Errorf("invalid -tui value %q (use simple, bubbletea, or telegram)", raw)
	}
}

func parseTelegramAllowedUsers(raw string) ([]int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	allowed := make([]int64, 0, len(parts))
	seen := make(map[int64]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid Telegram user ID %q", part)
		}
		if id == 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		allowed = append(allowed, id)
	}
	return allowed, nil
}
