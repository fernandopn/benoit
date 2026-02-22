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
	"github.com/fernandopn/benoit/middleware"
	"github.com/fernandopn/benoit/persistence"
	"github.com/fernandopn/benoit/providers"
	"github.com/fernandopn/benoit/session"
	"github.com/fernandopn/benoit/tools"
	filetools "github.com/fernandopn/benoit/tools/files"
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
	FSRootProvided             bool
	DBPath                     string
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
	defaultDBPath                     = "db.sqlite"
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

type ProviderStackOrchestrator interface {
	Build(ctx context.Context, cfg Config, toolSet []tools.Tool) (providers.Provider, func() error, error)
}

type defaultProviderStackOrchestrator struct{}

var providerStackOrchestrator ProviderStackOrchestrator = defaultProviderStackOrchestrator{}

func validateConfig(cfg Config) error {
	if cfg.Timeout < 0 {
		return fmt.Errorf("flag error: timeout cannot be negative")
	}
	if cfg.TelegramPollTimeoutSeconds < 0 {
		return fmt.Errorf("flag error: -telegram-poll-timeout cannot be negative")
	}

	switch cfg.Command {
	case CommandTUI:
		if err := validateProviderCommandConfig(cfg); err != nil {
			return err
		}
		if cfg.FSRootProvided && strings.TrimSpace(cfg.FSRoot) == "" {
			return fmt.Errorf("flag error: -fs-root cannot be empty")
		}
		if cfg.Render != RenderSimple && cfg.Render != RenderBubbleTea {
			return fmt.Errorf("flag error: invalid render mode %q", cfg.Render)
		}
		if err := session.ValidateSessionID(cfg.SessionID); err != nil {
			return fmt.Errorf("flag error: invalid -session-id: %w", err)
		}
		return nil
	case CommandChannelListener:
		if err := validateProviderCommandConfig(cfg); err != nil {
			return err
		}
		if cfg.Channel != ChannelTelegram {
			return fmt.Errorf("flag error: invalid channel %q", cfg.Channel)
		}
		if strings.TrimSpace(cfg.Credentials.TelegramBotToken) == "" {
			return fmt.Errorf("credential error: TELEGRAM_API_KEY is not set")
		}
		return nil
	case CommandListSessions:
		if strings.TrimSpace(cfg.DBPath) == "" {
			return fmt.Errorf("flag error: -db-path is required for list_sessions")
		}
		return nil
	default:
		return fmt.Errorf("flag error: invalid command %q", cfg.Command)
	}
}

func validateProviderCommandConfig(cfg Config) error {
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("flag error: model is required")
	}
	if strings.TrimSpace(cfg.Credentials.OpenAIAPIKey) == "" {
		return fmt.Errorf("credential error: OPENAI_API_KEY is not set")
	}
	return nil
}

func buildProvider(ctx context.Context, cfg Config) (providers.Provider, func() error, error) {
	toolSet, err := selectedTools(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("tool config error: %w", err)
	}
	return providerStackOrchestrator.Build(ctx, cfg, toolSet)
}

func (d defaultProviderStackOrchestrator) Build(ctx context.Context, cfg Config, toolSet []tools.Tool) (providers.Provider, func() error, error) {

	db, closeDB, err := persistence.ConfigureDB(ctx, strings.TrimSpace(cfg.DBPath))
	if err != nil {
		return nil, nil, fmt.Errorf("persistence init error: %w", err)
	}
	sessionStore, err := persistence.NewSessionStore(ctx, db)
	if err != nil {
		if closeDB != nil {
			_ = closeDB()
		}
		return nil, nil, fmt.Errorf("persistence init error: %w", err)
	}
	traceStore, err := persistence.NewTraceMessageStore(ctx, db)
	if err != nil {
		if closeDB != nil {
			_ = closeDB()
		}
		if sessionStore != nil {
			_ = sessionStore.Close()
		}
		return nil, nil, fmt.Errorf("trace persistence init error: %w", err)
	}
	middlewareFactories := buildSessionMiddleware(cfg, sessionStore, traceStore)

	routerCfg := session.Config{
		Model:                cfg.Model,
		OpenAIAPIKey:         cfg.Credentials.OpenAIAPIKey,
		OpenAIProviderParams: cfg.OpenAIProviderParams,
		SessionLookup:        sessionStoreLookupAdapter{store: sessionStore},
		MiddlewareFactories:  middlewareFactories,
		ProviderBuilder:      buildOpenAIProvider,
	}
	router, closeFactory, err := session.NewRouterProvider(ctx, routerCfg, toolSet)
	if err != nil {
		if closeDB != nil {
			_ = closeDB()
		}
		if sessionStore != nil {
			_ = sessionStore.Close()
		}
		return nil, nil, fmt.Errorf("provider init error: %w", err)
	}

	return router, combineCloseFuncs(closeFactory, closeDB), nil
}

type sessionStoreLookupAdapter struct {
	store persistence.SessionStore
}

func (s sessionStoreLookupAdapter) PreviousResponseID(ctx context.Context, providerType providers.ProviderType, sessionID string) (string, bool, error) {
	if s.store == nil {
		return "", false, nil
	}
	state, found, err := s.store.GetSession(ctx, providerType, sessionID)
	if err != nil {
		return "", false, err
	}
	return state.PreviousResponseID, found, nil
}

func buildSessionMiddleware(cfg Config, store persistence.SessionStore, traceStore persistence.TraceMessageStore) []session.MiddlewareFactory {
	middlewareFactories := make([]session.MiddlewareFactory, 0, 3)
	if store != nil {
		middlewareFactories = append(middlewareFactories, func(ctx context.Context, provider providers.Provider, providerType providers.ProviderType, sessionID string) (providers.Provider, error) {
			return middleware.NewSessionStoreMiddleware(provider, store, providerType, sessionID), nil
		})
	}

	if traceStore != nil {
		middlewareFactories = append(middlewareFactories, func(ctx context.Context, provider providers.Provider, providerType providers.ProviderType, sessionID string) (providers.Provider, error) {
			return middleware.NewPersistTrace(ctx, provider, providerType, sessionID, traceStore)
		})
	}
	if !cfg.BypassCompressionBarrier {
		middlewareFactories = append(middlewareFactories, func(ctx context.Context, provider providers.Provider, _ providers.ProviderType, _ string) (providers.Provider, error) {
			return middleware.NewCompressionBarrier(provider)
		})
	}
	return middlewareFactories
}

func buildOpenAIProvider(_ context.Context, model string, apiKey string, params providers.OpenAIProviderParams, toolSet []tools.Tool) (providers.Provider, func() error, error) {
	provider, err := providers.NewOpenAI(model, apiKey, params, toolSet)
	if err != nil {
		return nil, nil, err
	}
	return provider, nil, nil
}

func runCommand(ctx context.Context, cfg Config, provider providers.Provider) error {
	switch cfg.Command {
	case CommandTUI:
		return runTUICommand(ctx, cfg, provider)
	case CommandChannelListener:
		return runChannelListenerCommand(ctx, cfg, provider)
	default:
		return fmt.Errorf("flag error: invalid command %q", cfg.Command)
	}
}

func runTUICommand(ctx context.Context, cfg Config, provider providers.Provider) error {
	useSimpleMode := cfg.Render == RenderSimple
	if err := tui.Run(ctx, provider, cfg.Timeout, useSimpleMode, strings.TrimSpace(cfg.SessionID)); err != nil {
		return fmt.Errorf("tui error: %w", err)
	}
	return nil
}

func runChannelListenerCommand(ctx context.Context, cfg Config, provider providers.Provider) error {
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
}

func runListSessions(ctx context.Context, cfg Config) error {
	db, closeDB, err := persistence.ConfigureDB(ctx, strings.TrimSpace(cfg.DBPath))
	if err != nil {
		return fmt.Errorf("persistence init error: %w", err)
	}
	if closeDB != nil {
		defer closeDB()
	}

	store, err := persistence.NewSessionStore(ctx, db)
	if err != nil {
		return fmt.Errorf("persistence init error: %w", err)
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
	fileToolSet, err := configuredFileTools(cfg)
	if err != nil {
		return nil, err
	}
	if len(fileToolSet) > 0 {
		toolSet = append(toolSet, fileToolSet...)
	}

	channelBindings, err := configuredToolChannelBindings(cfg)
	if err != nil {
		return nil, err
	}
	if len(channelBindings) > 0 {
		toolSet = append(toolSet, tools.NewSendChannelMessageTool(channelBindings))
	}

	matonAPIKey := strings.TrimSpace(cfg.Credentials.MatonAPIKey)
	if matonAPIKey != "" {
		matonClient, err := tools.NewMatonClient(matonAPIKey, http.DefaultClient)
		if err != nil {
			return nil, err
		}
		toolSet = append(toolSet,
			tools.NewMatonGCalendarTool(matonClient),
			tools.NewMatonGmailTool(matonClient),
		)
	}

	return toolSet, nil
}

func configuredFileTools(cfg Config) ([]tools.Tool, error) {
	if cfg.Command != CommandTUI || !cfg.FSRootProvided {
		return nil, nil
	}
	return filetools.NewToolSet(cfg.FSRoot)
}

func configuredToolChannelBindings(cfg Config) ([]tools.ChannelBinding, error) {
	bindings := make([]tools.ChannelBinding, 0, 1)
	if cfg.Command != CommandTUI {
		return bindings, nil
	}
	telegramBotToken := strings.TrimSpace(cfg.Credentials.TelegramBotToken)
	if telegramBotToken == "" {
		return bindings, nil
	}
	telegramClient, err := channels.NewTelegram(telegramBotToken, http.DefaultClient)
	if err != nil {
		return nil, err
	}
	bindings = append(bindings, tools.ChannelBinding{Name: string(ChannelTelegram), Channel: telegramClient})
	return bindings, nil
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

type sharedStorageFlags struct {
	fsRoot *string
	dbPath *string
}

type sharedProviderFlags struct {
	storage                  sharedStorageFlags
	model                    *string
	timeout                  *time.Duration
	bypassCompressionBarrier *bool
}

func bindStorageFlags(flagSet *flag.FlagSet, defaultRoot string) sharedStorageFlags {
	return sharedStorageFlags{
		fsRoot: flagSet.String("fs-root", defaultRoot, "filesystem sandbox root (chroot for file tools)"),
		dbPath: flagSet.String("db-path", defaultDBPath, "db path for trace and session persistence"),
	}
}

func bindProviderFlags(flagSet *flag.FlagSet, defaultRoot string) sharedProviderFlags {
	storage := bindStorageFlags(flagSet, defaultRoot)
	return sharedProviderFlags{
		storage:                  storage,
		model:                    flagSet.String("model", "gpt-5.2", "model name"),
		timeout:                  flagSet.Duration("timeout", 20*time.Minute, "request timeout (e.g. 45s, 2m)"),
		bypassCompressionBarrier: flagSet.Bool("bypass-compression-barrier", false, "disable compression barrier middleware"),
	}
}

func parseFlagSet(flagSet *flag.FlagSet, args []string) error {
	if err := flagSet.Parse(args); err != nil {
		return err
	}
	if len(flagSet.Args()) > 0 {
		return fmt.Errorf("flag error: unexpected positional arguments: %s", strings.Join(flagSet.Args(), " "))
	}
	return nil
}

func flagIsSet(flagSet *flag.FlagSet, name string) bool {
	set := false
	flagSet.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func loadTUIConfig(defaultRoot string, args []string) (Config, error) {
	flagSet := flag.NewFlagSet(string(CommandTUI), flag.ContinueOnError)
	shared := bindProviderFlags(flagSet, defaultRoot)
	renderRaw := flagSet.String("render", defaultRenderMode, "render mode: simple or bubbletea")
	sessionID := flagSet.String("session-id", "", "resume an existing session ID")
	if err := parseFlagSet(flagSet, args); err != nil {
		return Config{}, err
	}
	fsRootProvided := flagIsSet(flagSet, "fs-root")
	renderMode, err := parseRenderMode(*renderRaw)
	if err != nil {
		return Config{}, fmt.Errorf("flag error: %w", err)
	}
	return Config{
		Command:                  CommandTUI,
		Render:                   renderMode,
		SessionID:                strings.TrimSpace(*sessionID),
		Model:                    strings.TrimSpace(*shared.model),
		Timeout:                  *shared.timeout,
		FSRoot:                   strings.TrimSpace(*shared.storage.fsRoot),
		FSRootProvided:           fsRootProvided,
		DBPath:                   strings.TrimSpace(*shared.storage.dbPath),
		BypassCompressionBarrier: *shared.bypassCompressionBarrier,
	}, nil
}

func loadChannelListenerConfig(defaultRoot string, args []string) (Config, error) {
	flagSet := flag.NewFlagSet(string(CommandChannelListener), flag.ContinueOnError)
	shared := bindProviderFlags(flagSet, defaultRoot)
	channelRaw := flagSet.String("channel", defaultChannelMode, "channel listener type: telegram")
	telegramPollTimeoutSeconds := flagSet.Int("telegram-poll-timeout", defaultTelegramPollTimeoutSeconds, "telegram getUpdates long poll timeout in seconds")
	telegramAllowedUsersRaw := flagSet.String("telegram-allowed-users", defaultTelegramAllowedUsers, "comma-separated Telegram user IDs allowed in telegram mode")
	if err := parseFlagSet(flagSet, args); err != nil {
		return Config{}, err
	}
	fsRootProvided := flagIsSet(flagSet, "fs-root")
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
		Model:                      strings.TrimSpace(*shared.model),
		Timeout:                    *shared.timeout,
		FSRoot:                     strings.TrimSpace(*shared.storage.fsRoot),
		FSRootProvided:             fsRootProvided,
		DBPath:                     strings.TrimSpace(*shared.storage.dbPath),
		BypassCompressionBarrier:   *shared.bypassCompressionBarrier,
		TelegramPollTimeoutSeconds: *telegramPollTimeoutSeconds,
		TelegramAllowedUserIDs:     allowedUsers,
	}, nil
}

func loadListSessionsConfig(defaultRoot string, args []string) (Config, error) {
	flagSet := flag.NewFlagSet(string(CommandListSessions), flag.ContinueOnError)
	storage := bindStorageFlags(flagSet, defaultRoot)
	if err := parseFlagSet(flagSet, args); err != nil {
		return Config{}, err
	}
	fsRootProvided := flagIsSet(flagSet, "fs-root")
	return Config{
		Command:        CommandListSessions,
		FSRoot:         strings.TrimSpace(*storage.fsRoot),
		FSRootProvided: fsRootProvided,
		DBPath:         strings.TrimSpace(*storage.dbPath),
	}, nil
}

func loadCredentials(cfg Config) (CredentialConfig, error) {
	creds := CredentialConfig{
		TelegramBotToken: strings.TrimSpace(os.Getenv(telegramAPIKeyEnv)),
		MatonAPIKey:      strings.TrimSpace(os.Getenv(matonAPIKeyEnv)),
	}

	if cfg.Command == CommandListSessions {
		return creds, nil
	}

	openAIAPIKey, err := requiredEnv(openAIAPIKeyEnv)
	if err != nil {
		return CredentialConfig{}, fmt.Errorf("credential error: %w", err)
	}
	creds.OpenAIAPIKey = openAIAPIKey

	if cfg.Command == CommandChannelListener && cfg.Channel == ChannelTelegram && creds.TelegramBotToken == "" {
		return CredentialConfig{}, fmt.Errorf("credential error: %s is not set", telegramAPIKeyEnv)
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
