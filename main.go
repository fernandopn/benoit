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
	"text/tabwriter"
	"time"

	"github.com/fernandopn/benoit/channels"
	"github.com/fernandopn/benoit/persistence"
	"github.com/fernandopn/benoit/providers"
	"github.com/fernandopn/benoit/session"
	"github.com/fernandopn/benoit/tools"
	"github.com/fernandopn/benoit/tui"
	"github.com/openai/openai-go/v3/shared"
)

type Command string

const (
	CommandTUI             Command = "tui"
	CommandListSessions    Command = "list_sessions"
	CommandChannelListener Command = "channel_listener"
)

type RenderMode string

const (
	RenderSimple    RenderMode = "simple"
	RenderBubbleTea RenderMode = "bubbletea"
)

type ChannelMode string

const (
	ChannelTelegram ChannelMode = "telegram"
)

type CredentialConfig struct {
	OpenAIAPIKey     string
	TelegramBotToken string
	MatonAPIKey      string
}

type Config struct {
	Command                    Command
	Render                     RenderMode
	Channel                    ChannelMode
	SessionID                  string
	Model                      string
	Timeout                    time.Duration
	FSRoot                     string
	TraceProviderDBPath        string
	SessionDBPath              string
	BypassCompressionBarrier   bool
	TelegramPollTimeoutSeconds int
	TelegramAllowedUserIDs     []int64
	Credentials                CredentialConfig
	OpenAIProviderParams       providers.OpenAIProviderParams
}

const (
	openAIReasoningEffort             = shared.ReasoningEffortHigh
	openAIReasoningSummary            = shared.ReasoningSummaryDetailed
	defaultRenderMode                 = string(RenderSimple)
	defaultChannelMode                = string(ChannelTelegram)
	defaultSessionDBPath              = "db.sqlite"
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

	cfg, err := loadConfig(defaultRoot, os.Args[1:])
	if err != nil {
		return err
	}

	creds, err := loadCredentials(cfg)
	if err != nil {
		return err
	}
	cfg.Credentials = creds
	cfg.OpenAIProviderParams = providers.OpenAIProviderParams{
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

	if cfg.Command == CommandListSessions {
		return runListSessions(ctx, cfg)
	}

	provider, closeProvider, err := buildProvider(ctx, cfg)
	if err != nil {
		return err
	}
	if closeProvider != nil {
		defer func() {
			if closeErr := closeProvider(); closeErr != nil {
				wrapped := fmt.Errorf("provider close error: %w", closeErr)
				if runErr == nil {
					runErr = wrapped
					return
				}
				runErr = errors.Join(runErr, wrapped)
			}
		}()
	}

	if err := runCommand(ctx, cfg, provider); err != nil {
		return err
	}
	return nil
}

func validateConfig(cfg Config) error {
	if cfg.Timeout < 0 {
		return fmt.Errorf("flag error: timeout cannot be negative")
	}
	if cfg.TelegramPollTimeoutSeconds < 0 {
		return fmt.Errorf("flag error: -telegram-poll-timeout cannot be negative")
	}

	switch cfg.Command {
	case CommandTUI:
		if strings.TrimSpace(cfg.Model) == "" {
			return fmt.Errorf("flag error: model is required")
		}
		if strings.TrimSpace(cfg.Credentials.OpenAIAPIKey) == "" {
			return fmt.Errorf("credential error: OPENAI_API_KEY is not set")
		}
		if cfg.Render != RenderSimple && cfg.Render != RenderBubbleTea {
			return fmt.Errorf("flag error: invalid render mode %q", cfg.Render)
		}
		return nil
	case CommandChannelListener:
		if strings.TrimSpace(cfg.Model) == "" {
			return fmt.Errorf("flag error: model is required")
		}
		if strings.TrimSpace(cfg.Credentials.OpenAIAPIKey) == "" {
			return fmt.Errorf("credential error: OPENAI_API_KEY is not set")
		}
		if cfg.Channel != ChannelTelegram {
			return fmt.Errorf("flag error: invalid channel %q", cfg.Channel)
		}
		if strings.TrimSpace(cfg.Credentials.TelegramBotToken) == "" {
			return fmt.Errorf("credential error: TELEGRAM_API_KEY is not set")
		}
		return nil
	case CommandListSessions:
		if strings.TrimSpace(cfg.SessionDBPath) == "" {
			return fmt.Errorf("flag error: -session-db-path is required for list_sessions")
		}
		return nil
	default:
		return fmt.Errorf("flag error: invalid command %q", cfg.Command)
	}
}

func buildProvider(ctx context.Context, cfg Config) (providers.Provider, func() error, error) {
	toolSet, err := selectedTools(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("tool config error: %w", err)
	}

	sessionStore, closeSessionStore, err := persistence.ConfigureSQLiteSessionStore(ctx, strings.TrimSpace(cfg.SessionDBPath))
	if err != nil {
		return nil, nil, fmt.Errorf("persistence init error: %w", err)
	}

	routerCfg := session.Config{
		Model:                    cfg.Model,
		OpenAIAPIKey:             cfg.Credentials.OpenAIAPIKey,
		OpenAIProviderParams:     cfg.OpenAIProviderParams,
		TraceProviderDBPath:      cfg.TraceProviderDBPath,
		BypassCompressionBarrier: cfg.BypassCompressionBarrier,
	}
	router, closeFactory, err := session.NewRouterProvider(ctx, routerCfg, toolSet, sessionStore)
	if err != nil {
		if closeSessionStore != nil {
			_ = closeSessionStore()
		}
		return nil, nil, fmt.Errorf("provider init error: %w", err)
	}

	return router, combineCloseFuncs(closeFactory, closeSessionStore), nil
}

func runCommand(ctx context.Context, cfg Config, provider providers.Provider) error {
	switch cfg.Command {
	case CommandTUI:
		useSimpleMode := cfg.Render == RenderSimple
		if err := tui.Run(ctx, provider, cfg.Timeout, useSimpleMode, strings.TrimSpace(cfg.SessionID)); err != nil {
			return fmt.Errorf("tui error: %w", err)
		}
		return nil
	case CommandChannelListener:
		switch cfg.Channel {
		case ChannelTelegram:
			telegramClient, err := channels.NewTelegram(cfg.Credentials.TelegramBotToken, http.DefaultClient)
			if err != nil {
				return fmt.Errorf("telegram init error: %w", err)
			}
			if err := tui.RunTelegram(ctx, telegramClient, provider, cfg.Timeout, cfg.TelegramPollTimeoutSeconds, cfg.TelegramAllowedUserIDs); err != nil {
				return fmt.Errorf("telegram listener error: %w", err)
			}
			return nil
		default:
			return fmt.Errorf("flag error: invalid channel %q", cfg.Channel)
		}
	default:
		return fmt.Errorf("flag error: invalid command %q", cfg.Command)
	}
}

func runListSessions(ctx context.Context, cfg Config) error {
	store, closeStore, err := persistence.ConfigureSQLiteSessionStore(ctx, strings.TrimSpace(cfg.SessionDBPath))
	if err != nil {
		return fmt.Errorf("persistence init error: %w", err)
	}
	if closeStore != nil {
		defer closeStore()
	}
	if store == nil {
		fmt.Println("No sessions found.")
		return nil
	}

	sessions, err := store.ListSessions(ctx, providers.ProviderTypeUnknown)
	if err != nil {
		return fmt.Errorf("list sessions error: %w", err)
	}
	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	fmt.Printf("Sessions (%d)\n", len(sessions))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tSESSION ID\tPREVIOUS RESPONSE ID\tTOKENS LEFT\tUPDATED AT")
	for _, state := range sessions {
		remaining := "-"
		if state.RemainingTokens != nil {
			remaining = strconv.FormatInt(*state.RemainingTokens, 10)
		}
		updatedAt := "-"
		if state.UpdatedAtUnix > 0 {
			updatedAt = time.Unix(state.UpdatedAtUnix, 0).UTC().Format(time.RFC3339)
		}
		previousResponseID := strings.TrimSpace(state.PreviousResponseID)
		if previousResponseID == "" {
			previousResponseID = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			state.Provider.String(),
			state.SessionID,
			previousResponseID,
			remaining,
			updatedAt,
		)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("list sessions output error: %w", err)
	}
	return nil
}

func combineCloseFuncs(closeFns ...func() error) func() error {
	nonNil := make([]func() error, 0, len(closeFns))
	for _, closeFn := range closeFns {
		if closeFn != nil {
			nonNil = append(nonNil, closeFn)
		}
	}
	if len(nonNil) == 0 {
		return nil
	}
	return func() error {
		var closeErr error
		for _, closeFn := range nonNil {
			if err := closeFn(); err != nil {
				closeErr = errors.Join(closeErr, err)
			}
		}
		return closeErr
	}
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

func loadConfig(defaultRoot string, args []string) (Config, error) {
	if len(args) == 0 {
		return Config{}, fmt.Errorf("flag error: command is required (use tui, list_sessions, or channel_listener)")
	}

	command := Command(strings.ToLower(strings.TrimSpace(args[0])))
	commandArgs := args[1:]

	switch command {
	case CommandTUI:
		return loadTUIConfig(defaultRoot, commandArgs)
	case CommandListSessions:
		return loadListSessionsConfig(defaultRoot, commandArgs)
	case CommandChannelListener:
		return loadChannelListenerConfig(defaultRoot, commandArgs)
	default:
		return Config{}, fmt.Errorf("flag error: invalid command %q (use tui, list_sessions, or channel_listener)", args[0])
	}
}

func loadTUIConfig(defaultRoot string, args []string) (Config, error) {
	flagSet := flag.NewFlagSet(string(CommandTUI), flag.ContinueOnError)
	model := flagSet.String("model", "gpt-5.2", "model name")
	timeout := flagSet.Duration("timeout", 20*time.Minute, "request timeout (e.g. 45s, 2m)")
	fsRoot := flagSet.String("fs-root", defaultRoot, "filesystem root")
	traceProviderDBPath := flagSet.String("trace_provider_db", "", "sqlite db path for provider trace logging")
	sessionDBPath := flagSet.String("session-db-path", defaultSessionDBPath, "sqlite db path for persisted provider sessions")
	bypassCompressionBarrier := flagSet.Bool("bypass-compression-barrier", false, "disable compression barrier middleware")
	renderRaw := flagSet.String("render", defaultRenderMode, "render mode: simple or bubbletea")
	sessionID := flagSet.String("session_id", "", "resume an existing session ID")
	if err := flagSet.Parse(args); err != nil {
		return Config{}, err
	}
	if len(flagSet.Args()) > 0 {
		return Config{}, fmt.Errorf("flag error: unexpected positional arguments: %s", strings.Join(flagSet.Args(), " "))
	}
	renderMode, err := parseRenderMode(*renderRaw)
	if err != nil {
		return Config{}, fmt.Errorf("flag error: %w", err)
	}
	return Config{
		Command:                  CommandTUI,
		Render:                   renderMode,
		SessionID:                strings.TrimSpace(*sessionID),
		Model:                    strings.TrimSpace(*model),
		Timeout:                  *timeout,
		FSRoot:                   strings.TrimSpace(*fsRoot),
		TraceProviderDBPath:      strings.TrimSpace(*traceProviderDBPath),
		SessionDBPath:            strings.TrimSpace(*sessionDBPath),
		BypassCompressionBarrier: *bypassCompressionBarrier,
	}, nil
}

func loadChannelListenerConfig(defaultRoot string, args []string) (Config, error) {
	flagSet := flag.NewFlagSet(string(CommandChannelListener), flag.ContinueOnError)
	channelRaw := flagSet.String("channel", defaultChannelMode, "channel listener type: telegram")
	model := flagSet.String("model", "gpt-5.2", "model name")
	timeout := flagSet.Duration("timeout", 20*time.Minute, "request timeout (e.g. 45s, 2m)")
	fsRoot := flagSet.String("fs-root", defaultRoot, "filesystem root")
	traceProviderDBPath := flagSet.String("trace_provider_db", "", "sqlite db path for provider trace logging")
	sessionDBPath := flagSet.String("session-db-path", defaultSessionDBPath, "sqlite db path for persisted provider sessions")
	bypassCompressionBarrier := flagSet.Bool("bypass-compression-barrier", false, "disable compression barrier middleware")
	telegramPollTimeoutSeconds := flagSet.Int("telegram-poll-timeout", defaultTelegramPollTimeoutSeconds, "telegram getUpdates long poll timeout in seconds")
	telegramAllowedUsersRaw := flagSet.String("telegram-allowed-users", defaultTelegramAllowedUsers, "comma-separated Telegram user IDs allowed in telegram mode")
	if err := flagSet.Parse(args); err != nil {
		return Config{}, err
	}
	if len(flagSet.Args()) > 0 {
		return Config{}, fmt.Errorf("flag error: unexpected positional arguments: %s", strings.Join(flagSet.Args(), " "))
	}
	channel, err := parseChannelMode(*channelRaw)
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
		Command:                    CommandChannelListener,
		Channel:                    channel,
		Model:                      strings.TrimSpace(*model),
		Timeout:                    *timeout,
		FSRoot:                     strings.TrimSpace(*fsRoot),
		TraceProviderDBPath:        strings.TrimSpace(*traceProviderDBPath),
		SessionDBPath:              strings.TrimSpace(*sessionDBPath),
		BypassCompressionBarrier:   *bypassCompressionBarrier,
		TelegramPollTimeoutSeconds: *telegramPollTimeoutSeconds,
		TelegramAllowedUserIDs:     allowedUsers,
	}, nil
}

func loadListSessionsConfig(defaultRoot string, args []string) (Config, error) {
	flagSet := flag.NewFlagSet(string(CommandListSessions), flag.ContinueOnError)
	fsRoot := flagSet.String("fs-root", defaultRoot, "filesystem root")
	sessionDBPath := flagSet.String("session-db-path", defaultSessionDBPath, "sqlite db path for persisted provider sessions")
	if err := flagSet.Parse(args); err != nil {
		return Config{}, err
	}
	if len(flagSet.Args()) > 0 {
		return Config{}, fmt.Errorf("flag error: unexpected positional arguments: %s", strings.Join(flagSet.Args(), " "))
	}
	return Config{
		Command:       CommandListSessions,
		FSRoot:        strings.TrimSpace(*fsRoot),
		SessionDBPath: strings.TrimSpace(*sessionDBPath),
	}, nil
}

func loadCredentials(cfg Config) (CredentialConfig, error) {
	creds := CredentialConfig{MatonAPIKey: strings.TrimSpace(os.Getenv(matonAPIKeyEnv))}

	if cfg.Command == CommandListSessions {
		return creds, nil
	}

	openAIAPIKey, err := requiredEnv(openAIAPIKeyEnv)
	if err != nil {
		return CredentialConfig{}, fmt.Errorf("credential error: %w", err)
	}
	creds.OpenAIAPIKey = openAIAPIKey

	if cfg.Command == CommandChannelListener && cfg.Channel == ChannelTelegram {
		telegramBotToken, err := requiredEnv(telegramAPIKeyEnv)
		if err != nil {
			return CredentialConfig{}, fmt.Errorf("credential error: %w", err)
		}
		creds.TelegramBotToken = telegramBotToken
	}

	return creds, nil
}

func requiredEnv(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is not set", name)
	}
	return value, nil
}

func parseRenderMode(raw string) (RenderMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(RenderSimple):
		return RenderSimple, nil
	case string(RenderBubbleTea):
		return RenderBubbleTea, nil
	default:
		return RenderSimple, fmt.Errorf("invalid --render value %q (use simple or bubbletea)", raw)
	}
}

func parseChannelMode(raw string) (ChannelMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ChannelTelegram):
		return ChannelTelegram, nil
	default:
		return ChannelTelegram, fmt.Errorf("invalid --channel value %q (use telegram)", raw)
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
