package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fernandopn/benoit/channels"
	"github.com/fernandopn/benoit/middleware"
	"github.com/fernandopn/benoit/providers"
	"github.com/fernandopn/benoit/tools"
	"github.com/fernandopn/benoit/tui"
	"github.com/openai/openai-go/v3/shared"
)

type Mode string

const (
	ModeSimple    Mode = "simple"
	ModeBubbleTea Mode = "bubbletea"
	ModeTelegram  Mode = "telegram"
)

type CredentialConfig struct {
	OpenAIAPIKey     string
	TelegramBotToken string
	MatonAPIKey      string
}

type Config struct {
	Model                      string
	Timeout                    time.Duration
	FSRoot                     string
	DBPath                     string
	Mode                       Mode
	TelegramPollTimeoutSeconds int
	TelegramAllowedUserIDs     []int64
	Credentials                CredentialConfig
	OpenAIParams               providers.OpenAIParams
}

const (
	openAIReasoningEffort             = shared.ReasoningEffortHigh
	openAIReasoningSummary            = shared.ReasoningSummaryDetailed
	defaultTUIMode                    = string(ModeSimple)
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

	if err := runWithConfig(context.Background(), cfg); err != nil {
		return err
	}
	return nil
}

func runWithConfig(ctx context.Context, cfg Config) (runErr error) {
	if err := validateConfig(cfg); err != nil {
		return err
	}

	initCtx := ctx
	initCancel := func() {}
	if cfg.Timeout > 0 {
		initCtx, initCancel = context.WithTimeout(ctx, cfg.Timeout)
	}
	defer initCancel()

	provider, closeMiddleware, err := buildProvider(initCtx, cfg)
	if err != nil {
		return err
	}
	if closeMiddleware != nil {
		defer func() {
			if closeErr := closeMiddleware(); closeErr != nil {
				wrapped := fmt.Errorf("middleware close error: %w", closeErr)
				if runErr == nil {
					runErr = wrapped
					return
				}
				runErr = errors.Join(runErr, wrapped)
			}
		}()
	}

	if err := runMode(ctx, cfg, provider); err != nil {
		return err
	}
	return nil
}

func validateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("flag error: model is required")
	}
	if cfg.Timeout < 0 {
		return fmt.Errorf("flag error: timeout cannot be negative")
	}
	if cfg.TelegramPollTimeoutSeconds < 0 {
		return fmt.Errorf("flag error: -telegram-poll-timeout cannot be negative")
	}
	if strings.TrimSpace(cfg.Credentials.OpenAIAPIKey) == "" {
		return fmt.Errorf("credential error: OPENAI_API_KEY is not set")
	}
	switch cfg.Mode {
	case ModeSimple, ModeBubbleTea:
		return nil
	case ModeTelegram:
		if strings.TrimSpace(cfg.Credentials.TelegramBotToken) == "" {
			return fmt.Errorf("credential error: TELEGRAM_API_KEY is not set")
		}
		return nil
	default:
		return fmt.Errorf("flag error: invalid mode %q", cfg.Mode)
	}
}

func buildProvider(ctx context.Context, cfg Config) (providers.Provider, func() error, error) {
	toolSet, err := selectedTools(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("tool config error: %w", err)
	}

	openAIProvider, err := providers.NewOpenAI(cfg.Model, cfg.Credentials.OpenAIAPIKey, cfg.OpenAIParams, toolSet)
	if err != nil {
		return nil, nil, fmt.Errorf("provider init error: %w", err)
	}
	var provider providers.Provider = openAIProvider

	provider, closeMiddleware, err := middleware.ConfigureSQLiteSave(ctx, provider, strings.TrimSpace(cfg.DBPath))
	if err != nil {
		return nil, nil, fmt.Errorf("middleware init error: %w", err)
	}
	return provider, closeMiddleware, nil
}

func selectedTools(cfg Config) ([]tools.Tool, error) {
	toolSet := []tools.Tool{
		tools.NewOpenAICodeInterpreterTool(),
		tools.NewOpenAIWebSearchTool(),
		tools.NewClockTool(),
	}

	matonAPIKey := strings.TrimSpace(cfg.Credentials.MatonAPIKey)
	if matonAPIKey == "" {
		return toolSet, nil
	}

	matonClient, err := tools.NewMatonClient(matonAPIKey, http.DefaultClient)
	if err != nil {
		return nil, err
	}
	toolSet = append(toolSet,
		tools.NewMatonGCalendarTool(matonClient),
		tools.NewMatonGmailTool(matonClient),
	)

	return toolSet, nil
}

func runMode(ctx context.Context, cfg Config, provider providers.Provider) error {
	switch cfg.Mode {
	case ModeTelegram:
		telegramClient, err := channels.NewTelegram(cfg.Credentials.TelegramBotToken, http.DefaultClient)
		if err != nil {
			return fmt.Errorf("telegram init error: %w", err)
		}
		if err := tui.RunTelegram(ctx, telegramClient, provider, cfg.Timeout, cfg.TelegramPollTimeoutSeconds, cfg.TelegramAllowedUserIDs); err != nil {
			return fmt.Errorf("telegram tui error: %w", err)
		}
		return nil
	case ModeSimple, ModeBubbleTea:
		useSimpleMode := cfg.Mode == ModeSimple
		if err := tui.Run(ctx, provider, cfg.Timeout, useSimpleMode); err != nil {
			return fmt.Errorf("tui error: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("flag error: invalid mode %q", cfg.Mode)
	}
}

func loadConfig(defaultRoot string) (Config, error) {
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
		return Config{}, fmt.Errorf("flag error: %w", err)
	}
	if *telegramPollTimeoutSeconds < 0 {
		return Config{}, fmt.Errorf("flag error: -telegram-poll-timeout cannot be negative")
	}
	allowedUsers, err := parseTelegramAllowedUsers(*telegramAllowedUsersRaw)
	if err != nil {
		return Config{}, fmt.Errorf("flag error: %w", err)
	}

	return Config{
		Model:                      strings.TrimSpace(*model),
		Timeout:                    *timeout,
		FSRoot:                     strings.TrimSpace(*fsRoot),
		DBPath:                     strings.TrimSpace(*dbPath),
		Mode:                       mode,
		TelegramPollTimeoutSeconds: *telegramPollTimeoutSeconds,
		TelegramAllowedUserIDs:     allowedUsers,
	}, nil
}

func loadCredentials(mode Mode) (CredentialConfig, error) {
	openAIAPIKey, err := requiredEnv(openAIAPIKeyEnv)
	if err != nil {
		return CredentialConfig{}, fmt.Errorf("credential error: %w", err)
	}
	telegramBotToken := ""
	if mode == ModeTelegram {
		telegramBotToken, err = requiredEnv(telegramAPIKeyEnv)
		if err != nil {
			return CredentialConfig{}, fmt.Errorf("credential error: %w", err)
		}
	}
	return CredentialConfig{
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

func parseTUIMode(raw string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ModeSimple):
		return ModeSimple, nil
	case string(ModeBubbleTea):
		return ModeBubbleTea, nil
	case string(ModeTelegram):
		return ModeTelegram, nil
	default:
		return ModeSimple, fmt.Errorf("invalid -tui value %q (use simple, bubbletea, or telegram)", raw)
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
