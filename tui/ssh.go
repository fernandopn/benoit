package tui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	wishbubbletea "github.com/charmbracelet/wish/bubbletea"
	"github.com/fernandopn/benoit/providers"
	"github.com/fernandopn/benoit/session"
	bubbleteaui "github.com/fernandopn/benoit/tui/bubbletea"
	tuicmd "github.com/fernandopn/benoit/tui/commands"
	tuiutils "github.com/fernandopn/benoit/tui/utils"
	gossh "golang.org/x/crypto/ssh"
)

const sshShutdownTimeout = 30 * time.Second

type SSHConfig struct {
	Address           string
	HostKeyPath       string
	AllowedPublicKeys []string
	Timeout           time.Duration
	UseSimple         bool
	SessionID         string
}

func RunSSH(ctx context.Context, provider providers.Provider, cfg SSHConfig) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if provider == nil {
		return errors.New("provider is required")
	}

	address := strings.TrimSpace(cfg.Address)
	if address == "" {
		return errors.New("ssh address is required")
	}
	hostKeyPath := strings.TrimSpace(cfg.HostKeyPath)
	if hostKeyPath == "" {
		return errors.New("ssh host key path is required")
	}
	allowedKeys := make([]ssh.PublicKey, 0, len(cfg.AllowedPublicKeys))
	for _, rawKey := range cfg.AllowedPublicKeys {
		rawKey = strings.TrimSpace(rawKey)
		if rawKey == "" {
			continue
		}
		parsedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(rawKey))
		if err != nil {
			return fmt.Errorf("invalid allowed ssh public key: %w", err)
		}
		allowedKeys = append(allowedKeys, parsedKey)
	}
	if len(allowedKeys) == 0 {
		return errors.New("at least one allowed ssh public key is required")
	}
	if err := os.MkdirAll(filepath.Dir(hostKeyPath), 0o700); err != nil {
		return fmt.Errorf("ssh host key directory error: %w", err)
	}

	options := []ssh.Option{
		wish.WithAddress(address),
		wish.WithHostKeyPath(hostKeyPath),
		wish.WithPublicKeyAuth(func(_ ssh.Context, candidate ssh.PublicKey) bool {
			for _, allowedKey := range allowedKeys {
				if ssh.KeysEqual(candidate, allowedKey) {
					return true
				}
			}
			return false
		}),
		wish.WithPasswordAuth(func(_ ssh.Context, _ string) bool { return false }),
		wish.WithKeyboardInteractiveAuth(func(_ ssh.Context, _ gossh.KeyboardInteractiveChallenge) bool { return false }),
	}

	if cfg.UseSimple {
		options = append(options, wish.WithMiddleware(simpleSSHMiddleware(provider, cfg.Timeout, cfg.SessionID)))
	} else {
		options = append(options, wish.WithMiddleware(
			wishbubbletea.Middleware(sshBubbleTeaHandler(provider, cfg.Timeout, cfg.SessionID)),
			activeterm.Middleware(),
		))
	}

	server, err := wish.NewServer(options...)
	if err != nil {
		return fmt.Errorf("ssh init error: %w", err)
	}

	return runSSHServer(ctx, server)
}

func runSSHServer(ctx context.Context, server *ssh.Server) error {
	if server == nil {
		return errors.New("ssh server is required")
	}

	listenErr := make(chan error, 1)
	go func() {
		listenErr <- server.ListenAndServe()
	}()

	select {
	case err := <-listenErr:
		if err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), sshShutdownTimeout)
		defer cancel()
		shutdownErr := server.Shutdown(shutdownCtx)
		serveErr := <-listenErr
		if serveErr != nil && !errors.Is(serveErr, ssh.ErrServerClosed) {
			if shutdownErr != nil {
				return errors.Join(
					fmt.Errorf("ssh server error: %w", serveErr),
					fmt.Errorf("ssh shutdown error: %w", shutdownErr),
				)
			}
			return fmt.Errorf("ssh server error: %w", serveErr)
		}
		if shutdownErr != nil && !errors.Is(shutdownErr, ssh.ErrServerClosed) {
			return fmt.Errorf("ssh shutdown error: %w", shutdownErr)
		}
		return ctx.Err()
	}
}

func sshBubbleTeaHandler(provider providers.Provider, timeout time.Duration, configuredSessionID string) wishbubbletea.Handler {
	return func(sess ssh.Session) (tea.Model, []tea.ProgramOption) {
		sessionID := resolveSSHSessionID(configuredSessionID)
		start := streamStartForProvider(provider, sessionID)

		cfg := bubbleteaui.Config{
			ProviderName: provider.Name(),
			WelcomeText:  bubbleteaui.DefaultWelcomeText,
			HelpText:     bubbleteaui.DefaultHelpText,
			StartStream: func(reqCtx context.Context, prompt string) (<-chan providers.Msg, context.CancelFunc, error) {
				streamCtx := reqCtx
				cancel := func() {}
				if timeout > 0 {
					streamCtx, cancel = context.WithTimeout(reqCtx, timeout)
				}
				stream := start(streamCtx, prompt)
				if stream == nil {
					cancel()
					return nil, func() {}, errors.New("provider stream is not configured")
				}
				return stream, cancel, nil
			},
		}

		model, err := bubbleteaui.NewModel(sess.Context(), cfg)
		if err != nil {
			fmt.Fprintf(sess.Stderr(), "session init error: %v\r\n", err)
			return nil, nil
		}

		return model, []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseCellMotion()}
	}
}

func simpleSSHMiddleware(provider providers.Provider, timeout time.Duration, configuredSessionID string) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sess ssh.Session) {
			runSimpleSSHSession(sess.Context(), sess, provider, timeout, configuredSessionID)
			next(sess)
		}
	}
}

func runSimpleSSHSession(ctx context.Context, sess ssh.Session, provider providers.Provider, timeout time.Duration, configuredSessionID string) {
	if sess == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reader := bufio.NewReader(sess)
	writer := bufio.NewWriter(sess)
	colors := newSimpleTheme(sshSessionHasPTY(sess))
	width := sshSessionWidth(sess)
	sessionID := resolveSSHSessionID(configuredSessionID)
	start := streamStartForProvider(provider, sessionID)

	writeSimpleHeader(writer, colors, provider.Name(), width)
	_ = writer.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		prompt := colors.Style(">: ", colors.Bold, colors.FGAccent)
		fmt.Fprint(writer, prompt)
		_ = writer.Flush()

		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Fprintln(sess.Stderr(), "read error:", err)
			return
		}

		text := strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(text) == "" {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(writer)
				_ = writer.Flush()
				return
			}
			continue
		}
		if tuicmd.IsExit(text) {
			return
		}

		_ = writer.Flush()

		var (
			hadError bool
			section  *providers.MsgType
		)

		switchState := func(next providers.MsgType) {
			if section != nil && *section == next {
				return
			}
			if section != nil {
				fmt.Fprintln(writer)
			}
			tt := next
			section = &tt
		}

		_, streamErr := streamPrompt(ctx, text, timeout, start, streamCallbacks{
			OnChat: func(value string) {
				switchState(providers.MsgTypeChatDelta)
				fmt.Fprint(writer, colors.Style(value, colors.FGStrong))
				_ = writer.Flush()
			},
			OnReasoning: func(value string) {
				switchState(providers.MsgTypeReasoningSummaryDelta)
				fmt.Fprint(writer, colors.Style(value, colors.FGMuted, colors.Dim))
				_ = writer.Flush()
			},
			OnToolCall: func(name string, args string, callID string) {
				_ = callID
				switchState(providers.MsgTypeToolCall)
				writeSimpleToolCard(writer, colors, width, name, args, "Running...")
				_ = writer.Flush()
			},
			OnToolResult: func(name string, args string, output string, callID string) {
				_ = callID
				switchState(providers.MsgTypeToolResult)
				writeSimpleToolCard(writer, colors, width, name, args, output)
				_ = writer.Flush()
			},
			OnContextUsage: func(value string, metadata map[string]string) {
				switchState(providers.MsgTypeContextUsage)
				if left, ok := tuiutils.ContextLeftPercent(value, metadata); ok {
					fmt.Fprintln(writer, colors.Style(formatContextLeft(left), colors.FGAccent, colors.Dim))
				}
				_ = writer.Flush()
			},
			OnCompressionStatus: func(value string, metadata map[string]string) {
				_ = metadata
				switchState(providers.MsgTypeCompressionStatus)
				fmt.Fprintln(writer, colors.Style(value, colors.FGAccent, colors.Dim))
				_ = writer.Flush()
			},
			OnError: func(errText string) {
				hadError = true
				fmt.Fprintln(sess.Stderr(), colors.Style("request error:", colors.Bold, colors.FGWarn), errText)
			},
		})
		if streamErr != nil && !hadError {
			hadError = true
			fmt.Fprintln(sess.Stderr(), colors.Style("request error:", colors.Bold, colors.FGWarn), streamErr)
		}

		if section != nil {
			fmt.Fprintln(writer)
		}
		_ = writer.Flush()

		if hadError && errors.Is(err, io.EOF) {
			return
		}

		if errors.Is(err, io.EOF) {
			return
		}
	}
}

func resolveSSHSessionID(configuredSessionID string) string {
	return session.ResolveTUISessionID(configuredSessionID)
}

func sshSessionHasPTY(sess ssh.Session) bool {
	if sess == nil {
		return false
	}
	_, _, ok := sess.Pty()
	return ok
}

func sshSessionWidth(sess ssh.Session) int {
	if sess == nil {
		return 0
	}
	pty, _, ok := sess.Pty()
	if !ok {
		return 0
	}
	if pty.Window.Width <= 0 {
		return 0
	}
	return pty.Window.Width
}
