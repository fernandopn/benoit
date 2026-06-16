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
	gossh "golang.org/x/crypto/ssh"
)

type Command string

const (
	CommandTUI             Command = "tui"
	CommandSSH             Command = "ssh"
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

type ProviderName string

const (
	ProviderOpenAI     ProviderName = "openai"
	ProviderOpenRouter ProviderName = "openrouter"
)

type CredentialConfig struct {
	OpenAIAPIKey            string
	OpenRouterAPIKey        string
	TelegramBotToken        string
	MatonAPIKey             string
	TelegramAllowedUserIDs  []int64
	SSHAllowedPublicKeyList []string
}

type Config struct {
	Command                    Command
	Render                     RenderMode
	Channel                    ChannelMode
	Provider                   ProviderName
	SessionID                  string
	SSHPort                    int
	SSHAllowedPublicKeys       []string
	EnvFilePath                string
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
	defaultProviderName               = string(ProviderOpenAI)
	defaultOpenAIModel                = "gpt-5.5"
	defaultOpenRouterModel            = "z-ai/glm-5.1"
	defaultSSHPort                    = 23234
	defaultSSHHostKeyPath             = "data/.ssh/host_ed25519"
	defaultEnvFilePath                = ".env"
	defaultDBPath                     = "db.sqlite"
	defaultTelegramPollTimeoutSeconds = 30

	openAIAPIKeyEnv         = "OPENAI_API_KEY"
	openRouterAPIKeyEnv     = "OPENROUTER_API_KEY"
	telegramAPIKeyEnv       = "TELEGRAM_API_KEY"
	matonAPIKeyEnv          = "MATON_API_KEY"
	telegramAllowedUsersEnv = "TELEGRAM_ALLOWED_USERS"
	sshAllowedPublicKeysEnv = "SSH_ALLOWED_PUBLIC_KEYS"
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

	envFileValues, err := loadDotEnvIfExists(cfg.EnvFilePath)
	if err != nil {
		return err
	}

	creds, err := loadCredentials(cfg, envFileValues)
	if err != nil {
		return err
	}
	cfg.Credentials = creds
	cfg.TelegramAllowedUserIDs = creds.TelegramAllowedUserIDs
	cfg.SSHAllowedPublicKeys = creds.SSHAllowedPublicKeyList
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
	case CommandTUI, CommandSSH:
		if err := validateProviderCommandConfig(cfg); err != nil {
			return err
		}
		if cfg.FSRootProvided && strings.TrimSpace(cfg.FSRoot) == "" {
			return fmt.Errorf("flag error: -fs-root cannot be empty")
		}
		if err := session.ValidateSessionID(cfg.SessionID); err != nil {
			return fmt.Errorf("flag error: invalid -session-id: %w", err)
		}
		if cfg.Command == CommandTUI {
			if cfg.Render != RenderSimple && cfg.Render != RenderBubbleTea {
				return fmt.Errorf("flag error: invalid render mode %q", cfg.Render)
			}
		}
		if cfg.Command == CommandSSH {
			if cfg.Render != RenderBubbleTea {
				return fmt.Errorf("flag error: ssh render mode is fixed to bubbletea")
			}
			if len(cfg.SSHAllowedPublicKeys) == 0 {
				return fmt.Errorf("credential error: %s is not set", sshAllowedPublicKeysEnv)
			}
			if err := validateSSHPort(cfg.SSHPort); err != nil {
				return err
			}
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
	switch cfg.Provider {
	case ProviderOpenRouter:
		if strings.TrimSpace(cfg.Credentials.OpenRouterAPIKey) == "" {
			return fmt.Errorf("credential error: %s is not set", openRouterAPIKeyEnv)
		}
	default:
		if strings.TrimSpace(cfg.Credentials.OpenAIAPIKey) == "" {
			return fmt.Errorf("credential error: %s is not set", openAIAPIKeyEnv)
		}
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

	providerType, apiKey, providerBuilder := selectProvider(cfg)
	routerCfg := session.Config{
		Model:                cfg.Model,
		OpenAIAPIKey:         apiKey,
		ProviderType:         providerType,
		OpenAIProviderParams: cfg.OpenAIProviderParams,
		SessionLookup:        sessionStoreLookupAdapter{store: sessionStore},
		MiddlewareFactories:  middlewareFactories,
		ProviderBuilder:      providerBuilder,
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

func (s sessionStoreLookupAdapter) PreviousResponse(ctx context.Context, providerType providers.ProviderType, sessionID string) (string, bool, error) {
	if s.store == nil {
		return "", false, nil
	}
	state, found, err := s.store.GetSession(ctx, providerType, sessionID)
	if err != nil {
		return "", false, err
	}
	return state.PreviousResponse, found, nil
}

func selectProvider(cfg Config) (providers.ProviderType, string, session.ProviderBuilder) {
	if cfg.Provider == ProviderOpenRouter {
		return providers.ProviderTypeOpenRouter, cfg.Credentials.OpenRouterAPIKey, buildOpenRouterProvider
	}
	return providers.ProviderTypeOpenAI, cfg.Credentials.OpenAIAPIKey, buildOpenAIProvider
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

func buildOpenRouterProvider(ctx context.Context, model string, apiKey string, params providers.OpenAIProviderParams, toolSet []tools.Tool) (providers.Provider, func() error, error) {
	provider, err := providers.NewOpenRouter(ctx, model, apiKey, params, toolSet)
	if err != nil {
		return nil, nil, err
	}
	return provider, nil, nil
}

func runCommand(ctx context.Context, cfg Config, provider providers.Provider) error {
	switch cfg.Command {
	case CommandTUI:
		return runTUICommand(ctx, cfg, provider)
	case CommandSSH:
		return runSSHCommand(ctx, cfg, provider)
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

func runSSHCommand(ctx context.Context, cfg Config, provider providers.Provider) error {
	port := cfg.SSHPort
	if port == 0 {
		port = defaultSSHPort
	}
	address := fmt.Sprintf(":%d", port)
	fmt.Printf("SSH server listening on port %d\n", port)

	sshCfg := tui.SSHConfig{
		Address:           address,
		HostKeyPath:       defaultSSHHostKeyPath,
		AllowedPublicKeys: cfg.SSHAllowedPublicKeys,
		Timeout:           cfg.Timeout,
		UseSimple:         false,
		SessionID:         strings.TrimSpace(cfg.SessionID),
	}
	if err := tui.RunSSH(ctx, provider, sshCfg); err != nil {
		return fmt.Errorf("ssh error: %w", err)
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
	fmt.Fprintln(w, "PROVIDER\tSESSION ID\tPREVIOUS RESPONSE\tTOKENS LEFT\tUPDATED AT")
	for _, state := range sessions {
		remaining := "-"
		if state.RemainingTokens != nil {
			remaining = strconv.FormatInt(*state.RemainingTokens, 10)
		}
		updatedAt := "-"
		if state.UpdatedAtUnix > 0 {
			updatedAt = time.Unix(state.UpdatedAtUnix, 0).UTC().Format(time.RFC3339)
		}
		previousResponse := truncateForDisplay(strings.TrimSpace(state.PreviousResponse), 60)
		if previousResponse == "" {
			previousResponse = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			state.Provider.String(),
			state.SessionID,
			previousResponse,
			remaining,
			updatedAt,
		)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("list sessions output error: %w", err)
	}
	return nil
}

func truncateForDisplay(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
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
	if !isInteractiveCommand(cfg.Command) || !cfg.FSRootProvided {
		return nil, nil
	}
	return filetools.NewToolSet(cfg.FSRoot)
}

func configuredToolChannelBindings(cfg Config) ([]tools.ChannelBinding, error) {
	bindings := make([]tools.ChannelBinding, 0, 1)
	if !isInteractiveCommand(cfg.Command) {
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
		return Config{}, fmt.Errorf("flag error: command is required (use tui, ssh, list_sessions, or channel_listener)")
	}

	command := Command(strings.ToLower(strings.TrimSpace(args[0])))
	commandArgs := args[1:]

	switch command {
	case CommandTUI:
		return loadTUIConfig(defaultRoot, commandArgs)
	case CommandSSH:
		return loadSSHConfig(defaultRoot, commandArgs)
	case CommandListSessions:
		return loadListSessionsConfig(defaultRoot, commandArgs)
	case CommandChannelListener:
		return loadChannelListenerConfig(defaultRoot, commandArgs)
	default:
		return Config{}, fmt.Errorf("flag error: invalid command %q (use tui, ssh, list_sessions, or channel_listener)", args[0])
	}
}

type sharedStorageFlags struct {
	fsRoot  *string
	dbPath  *string
	envFile *string
}

type sharedProviderFlags struct {
	storage                  sharedStorageFlags
	model                    *string
	provider                 *string
	timeout                  *time.Duration
	bypassCompressionBarrier *bool
}

func bindStorageFlags(flagSet *flag.FlagSet, defaultRoot string) sharedStorageFlags {
	return sharedStorageFlags{
		fsRoot:  flagSet.String("fs-root", defaultRoot, "filesystem sandbox root (chroot for file tools)"),
		dbPath:  flagSet.String("db-path", defaultDBPath, "db path for trace and session persistence"),
		envFile: flagSet.String("env-file", defaultEnvFilePath, "optional .env file path for credentials"),
	}
}

func bindProviderFlags(flagSet *flag.FlagSet, defaultRoot string) sharedProviderFlags {
	storage := bindStorageFlags(flagSet, defaultRoot)
	return sharedProviderFlags{
		storage:                  storage,
		model:                    flagSet.String("model", "", "model name (default: gpt-5.5 for openai, z-ai/glm-5.1 for openrouter)"),
		provider:                 flagSet.String("provider", defaultProviderName, "llm provider: openai or openrouter"),
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
	return loadInteractiveConfig(CommandTUI, defaultRoot, args)
}

func loadSSHConfig(defaultRoot string, args []string) (Config, error) {
	return loadInteractiveConfig(CommandSSH, defaultRoot, args)
}

func loadInteractiveConfig(command Command, defaultRoot string, args []string) (Config, error) {
	if !isInteractiveCommand(command) {
		return Config{}, fmt.Errorf("flag error: unsupported interactive command %q", command)
	}
	flagSet := flag.NewFlagSet(string(command), flag.ContinueOnError)
	shared := bindProviderFlags(flagSet, defaultRoot)
	var renderRaw *string
	if command == CommandTUI {
		renderRaw = flagSet.String("render", defaultRenderMode, "render mode: simple or bubbletea")
	}
	sessionID := flagSet.String("session-id", "", "resume an existing session ID")
	var sshPort *int
	if command == CommandSSH {
		sshPort = flagSet.Int("ssh-port", defaultSSHPort, "ssh listen port")
	}
	if err := parseFlagSet(flagSet, args); err != nil {
		return Config{}, err
	}
	fsRootProvided := flagIsSet(flagSet, "fs-root")
	renderMode := RenderBubbleTea
	if renderRaw != nil {
		parsedRenderMode, err := parseRenderMode(*renderRaw)
		if err != nil {
			return Config{}, fmt.Errorf("flag error: %w", err)
		}
		renderMode = parsedRenderMode
	}
	provider, err := parseProviderName(*shared.provider)
	if err != nil {
		return Config{}, fmt.Errorf("flag error: %w", err)
	}
	model := strings.TrimSpace(*shared.model)
	if model == "" {
		model = defaultModelForProvider(provider)
	}
	cfg := Config{
		Command:                  command,
		Render:                   renderMode,
		Provider:                 provider,
		SessionID:                strings.TrimSpace(*sessionID),
		Model:                    model,
		Timeout:                  *shared.timeout,
		EnvFilePath:              strings.TrimSpace(*shared.storage.envFile),
		FSRoot:                   strings.TrimSpace(*shared.storage.fsRoot),
		FSRootProvided:           fsRootProvided,
		DBPath:                   strings.TrimSpace(*shared.storage.dbPath),
		BypassCompressionBarrier: *shared.bypassCompressionBarrier,
	}
	if sshPort != nil {
		cfg.SSHPort = *sshPort
	}
	return cfg, nil
}

func loadChannelListenerConfig(defaultRoot string, args []string) (Config, error) {
	flagSet := flag.NewFlagSet(string(CommandChannelListener), flag.ContinueOnError)
	shared := bindProviderFlags(flagSet, defaultRoot)
	channelRaw := flagSet.String("channel", defaultChannelMode, "channel listener type: telegram")
	telegramPollTimeoutSeconds := flagSet.Int("telegram-poll-timeout", defaultTelegramPollTimeoutSeconds, "telegram getUpdates long poll timeout in seconds")
	if err := parseFlagSet(flagSet, args); err != nil {
		return Config{}, err
	}
	fsRootProvided := flagIsSet(flagSet, "fs-root")
	channel, err := parseChannelMode(*channelRaw)
	if err != nil {
		return Config{}, fmt.Errorf("flag error: %w", err)
	}
	provider, err := parseProviderName(*shared.provider)
	if err != nil {
		return Config{}, fmt.Errorf("flag error: %w", err)
	}
	model := strings.TrimSpace(*shared.model)
	if model == "" {
		model = defaultModelForProvider(provider)
	}
	if *telegramPollTimeoutSeconds < 0 {
		return Config{}, fmt.Errorf("flag error: -telegram-poll-timeout cannot be negative")
	}
	return Config{
		Command:                    CommandChannelListener,
		Channel:                    channel,
		Provider:                   provider,
		Model:                      model,
		Timeout:                    *shared.timeout,
		EnvFilePath:                strings.TrimSpace(*shared.storage.envFile),
		FSRoot:                     strings.TrimSpace(*shared.storage.fsRoot),
		FSRootProvided:             fsRootProvided,
		DBPath:                     strings.TrimSpace(*shared.storage.dbPath),
		BypassCompressionBarrier:   *shared.bypassCompressionBarrier,
		TelegramPollTimeoutSeconds: *telegramPollTimeoutSeconds,
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
		EnvFilePath:    strings.TrimSpace(*storage.envFile),
		FSRoot:         strings.TrimSpace(*storage.fsRoot),
		FSRootProvided: fsRootProvided,
		DBPath:         strings.TrimSpace(*storage.dbPath),
	}, nil
}

func loadCredentials(cfg Config, envFileValues map[string]string) (CredentialConfig, error) {
	creds := CredentialConfig{
		TelegramBotToken: lookupEnvValue(telegramAPIKeyEnv, envFileValues),
		MatonAPIKey:      lookupEnvValue(matonAPIKeyEnv, envFileValues),
	}

	if cfg.Command == CommandChannelListener {
		allowedUsers, err := parseTelegramAllowedUsers(lookupEnvValue(telegramAllowedUsersEnv, envFileValues))
		if err != nil {
			return CredentialConfig{}, fmt.Errorf("invalid %s: %w", telegramAllowedUsersEnv, err)
		}
		creds.TelegramAllowedUserIDs = allowedUsers
	}

	if cfg.Command == CommandSSH {
		allowedKeys, err := parseSSHAllowedPublicKeys(lookupEnvValue(sshAllowedPublicKeysEnv, envFileValues))
		if err != nil {
			return CredentialConfig{}, fmt.Errorf("invalid %s: %w", sshAllowedPublicKeysEnv, err)
		}
		if len(allowedKeys) == 0 {
			return CredentialConfig{}, fmt.Errorf("%s is not set", sshAllowedPublicKeysEnv)
		}
		creds.SSHAllowedPublicKeyList = allowedKeys
	}

	if cfg.Command == CommandListSessions {
		return creds, nil
	}

	switch cfg.Provider {
	case ProviderOpenRouter:
		openRouterAPIKey, err := requiredEnv(openRouterAPIKeyEnv, envFileValues)
		if err != nil {
			return CredentialConfig{}, fmt.Errorf("credential error: %w", err)
		}
		creds.OpenRouterAPIKey = openRouterAPIKey
	default:
		openAIAPIKey, err := requiredEnv(openAIAPIKeyEnv, envFileValues)
		if err != nil {
			return CredentialConfig{}, fmt.Errorf("credential error: %w", err)
		}
		creds.OpenAIAPIKey = openAIAPIKey
	}

	if cfg.Command == CommandChannelListener && cfg.Channel == ChannelTelegram && creds.TelegramBotToken == "" {
		return CredentialConfig{}, fmt.Errorf("credential error: %s is not set", telegramAPIKeyEnv)
	}

	return creds, nil
}

func parseSSHAllowedPublicKeys(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	keys := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for idx, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		if _, _, _, _, err := gossh.ParseAuthorizedKey([]byte(part)); err != nil {
			return nil, fmt.Errorf("invalid SSH public key at position %d", idx+1)
		}
		seen[part] = struct{}{}
		keys = append(keys, part)
	}
	return keys, nil
}

func requiredEnv(name string, envFileValues map[string]string) (string, error) {
	value := lookupEnvValue(name, envFileValues)
	if value == "" {
		return "", fmt.Errorf("%s is not set", name)
	}
	return value, nil
}

func lookupEnvValue(name string, envFileValues map[string]string) string {
	if envFileValues != nil {
		if value, ok := envFileValues[name]; ok {
			return strings.TrimSpace(value)
		}
	}
	return strings.TrimSpace(os.Getenv(name))
}

func loadDotEnvIfExists(path string) (map[string]string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("env file read error: %w", err)
	}

	values := make(map[string]string)
	lines := strings.Split(string(content), "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("env file parse error at line %d: missing '='", i+1)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("env file parse error at line %d: empty key", i+1)
		}
		value = strings.TrimSpace(value)

		parsedValue, parseErr := parseDotEnvValue(value)
		if parseErr != nil {
			return nil, fmt.Errorf("env file parse error at line %d: %w", i+1, parseErr)
		}
		values[key] = parsedValue
	}

	return values, nil
}

func parseDotEnvValue(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) >= 2 {
		if value[0] == '"' && value[len(value)-1] == '"' {
			unquoted, err := strconv.Unquote(value)
			if err != nil {
				return "", err
			}
			return unquoted, nil
		}
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1], nil
		}
	}

	if commentIndex := strings.Index(value, " #"); commentIndex >= 0 {
		value = strings.TrimSpace(value[:commentIndex])
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

func defaultModelForProvider(provider ProviderName) string {
	if provider == ProviderOpenRouter {
		return defaultOpenRouterModel
	}
	return defaultOpenAIModel
}

func parseProviderName(raw string) (ProviderName, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ProviderOpenAI):
		return ProviderOpenAI, nil
	case string(ProviderOpenRouter):
		return ProviderOpenRouter, nil
	default:
		return ProviderOpenAI, fmt.Errorf("invalid --provider value %q (use openai or openrouter)", raw)
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

func validateSSHPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("flag error: -ssh-port must be between 1 and 65535")
	}
	return nil
}

func isInteractiveCommand(command Command) bool {
	return command == CommandTUI || command == CommandSSH
}
