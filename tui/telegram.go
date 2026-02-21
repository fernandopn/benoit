package tui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fernandopn/benoit/channels"
	"github.com/fernandopn/benoit/providers"
	"github.com/fernandopn/benoit/session"
	simpleui "github.com/fernandopn/benoit/tui/simple"
	tuiutils "github.com/fernandopn/benoit/tui/utils"
	"golang.org/x/term"
)

const telegramMaxMessageLength = 4096
const telegramTypingInterval = 4 * time.Second
const telegramTypingRequestTimeout = 4 * time.Second

func RunTelegram(ctx context.Context, telegramChannel channels.Channel, provider providers.Provider, timeout time.Duration, pollTimeoutSeconds int, allowedUserIDs []int64) error {
	if telegramChannel == nil {
		return errors.New("telegram client is required")
	}
	if provider == nil {
		return errors.New("provider is required")
	}
	if pollTimeoutSeconds < 0 {
		return errors.New("poll timeout seconds cannot be negative")
	}

	writer := bufio.NewWriter(os.Stdout)
	colors := newSimpleTheme(term.IsTerminal(int(os.Stdout.Fd())))
	width := simpleTerminalWidth()
	writeTelegramHeader(writer, colors, provider.Name(), width)
	writer.Flush()
	allowedUsers := buildAllowedTelegramUsers(allowedUserIDs)
	incomingMessages := make(chan channels.ChannelMessage)
	if err := telegramChannel.RegisterReceiveMessageChan(incomingMessages); err != nil {
		return err
	}
	receiveCtx, stopReceiving := context.WithCancel(ctx)
	defer stopReceiving()

	receiveErr := make(chan error, 1)
	go func() {
		receiveErr <- telegramChannel.Listen(receiveCtx, pollTimeoutSeconds)
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-receiveErr:
			if err == nil {
				return errors.New("telegram receiver stopped")
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return err
		case incoming, ok := <-incomingMessages:
			if !ok {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				return errors.New("telegram receiver channel closed")
			}
			if incoming.Type != channels.TextMessage {
				continue
			}
			if !isTelegramUserAllowed(incoming.UserID, allowedUsers) {
				continue
			}
			prompt := strings.TrimSpace(incoming.Text)
			if prompt == "" {
				continue
			}

			writeTelegramIncoming(writer, colors, incoming)
			writer.Flush()
			if err := sendTelegramTypingSignal(ctx, telegramChannel, incoming.UserID, true); err != nil && ctx.Err() == nil {
				fmt.Fprintln(os.Stderr, "telegram typing error:", err)
			}

			typingCtx, stopTyping := context.WithCancel(ctx)
			typingDone := make(chan struct{})
			go func() {
				runTelegramTypingLoop(typingCtx, telegramChannel, incoming.UserID)
				close(typingDone)
			}()

			sessionID := telegramSessionID(incoming.UserID)
			reply, err := runTelegramPromptWithOutput(ctx, provider, prompt, timeout, sessionID, writer, colors, width)
			stopTyping()
			<-typingDone
			if err := sendTelegramTypingSignal(ctx, telegramChannel, incoming.UserID, false); err != nil && ctx.Err() == nil {
				fmt.Fprintln(os.Stderr, "telegram typing error:", err)
			}
			if err != nil {
				reply = "request error: " + err.Error()
			}

			if err := sendTelegramReply(ctx, telegramChannel, incoming.UserID, reply); err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				return err
			}
		}
	}
}

func runTelegramPrompt(ctx context.Context, provider providers.Provider, prompt string, timeout time.Duration) (string, error) {
	return runTelegramPromptWithOutput(ctx, provider, prompt, timeout, "", nil, simpleTheme{}, 0)
}

func buildAllowedTelegramUsers(ids []int64) map[int64]struct{} {
	if len(ids) == 0 {
		return nil
	}
	allowed := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		allowed[id] = struct{}{}
	}
	if len(allowed) == 0 {
		return nil
	}
	return allowed
}

func isTelegramUserAllowed(userID int64, allowed map[int64]struct{}) bool {
	if len(allowed) == 0 {
		return true
	}
	if userID == 0 {
		return false
	}
	_, ok := allowed[userID]
	return ok
}

func runTelegramPromptWithOutput(ctx context.Context, provider providers.Provider, prompt string, timeout time.Duration, sessionID string, writer *bufio.Writer, colors simpleTheme, width int) (string, error) {
	var section *providers.MsgType

	switchState := func(next providers.MsgType) {
		if writer == nil {
			return
		}
		if section != nil && *section == next {
			return
		}
		if section != nil {
			fmt.Fprintln(writer)
		}
		tt := next
		section = &tt
	}

	reply, err := streamPrompt(ctx, prompt, timeout, streamStartForProvider(provider, sessionID), streamCallbacks{
		OnChat: func(value string) {
			switchState(providers.MsgTypeChatDelta)
			if writer != nil {
				fmt.Fprint(writer, colors.Style(value, colors.FGStrong))
				writer.Flush()
			}
		},
		OnReasoning: func(value string) {
			switchState(providers.MsgTypeReasoningSummaryDelta)
			if writer != nil {
				fmt.Fprint(writer, colors.Style(value, colors.FGMuted, colors.Dim))
				writer.Flush()
			}
		},
		OnToolCall: func(name string, args string, callID string) {
			_ = callID
			switchState(providers.MsgTypeToolCall)
			if writer != nil {
				writeSimpleToolCard(writer, colors, width, name, args, "Running...")
				writer.Flush()
			}
		},
		OnToolResult: func(name string, args string, output string, callID string) {
			_ = callID
			switchState(providers.MsgTypeToolResult)
			if writer != nil {
				writeSimpleToolCard(writer, colors, width, name, args, output)
				writer.Flush()
			}
		},
		OnContextUsage: func(value string, metadata map[string]string) {
			switchState(providers.MsgTypeContextUsage)
			if writer != nil {
				if left, ok := tuiutils.ContextLeftPercent(value, metadata); ok {
					fmt.Fprintln(writer, colors.Style(formatContextLeft(left), colors.FGAccent, colors.Dim))
					writer.Flush()
				}
			}
		},
		OnCompressionStatus: func(value string, metadata map[string]string) {
			_ = metadata
			switchState(providers.MsgTypeCompressionStatus)
			if writer != nil {
				fmt.Fprintln(writer, colors.Style(value, colors.FGAccent, colors.Dim))
				writer.Flush()
			}
		},
		OnError: func(errText string) {
			if writer != nil {
				fmt.Fprintln(os.Stderr, colors.Style("request error:", colors.Bold, colors.FGWarn), errText)
			}
		},
	})
	if writer != nil && section != nil {
		fmt.Fprintln(writer)
		writer.Flush()
	}
	if err != nil {
		return "", err
	}
	return reply, nil
}

func sendTelegramReply(ctx context.Context, telegramChannel channels.Channel, userID int64, text string) error {
	chunks := splitTelegramMessage(text, telegramMaxMessageLength)
	for _, chunk := range chunks {
		err := telegramChannel.SendMessage(ctx, channels.ChannelMessage{Text: chunk, UserID: userID, Type: channels.TextMessage})
		if err != nil {
			return err
		}
	}
	return nil
}

func splitTelegramMessage(text string, maxCharacters int) []string {
	if maxCharacters <= 0 {
		if strings.TrimSpace(text) == "" {
			return []string{"(empty response)"}
		}
		return []string{text}
	}

	if strings.TrimSpace(text) == "" {
		return []string{"(empty response)"}
	}

	runes := []rune(text)
	if len(runes) <= maxCharacters {
		return []string{text}
	}

	chunks := make([]string, 0, (len(runes)/maxCharacters)+1)
	for len(runes) > maxCharacters {
		chunks = append(chunks, string(runes[:maxCharacters]))
		runes = runes[maxCharacters:]
	}
	if len(runes) > 0 {
		chunks = append(chunks, string(runes))
	}
	return chunks
}

func telegramSessionID(userID int64) string {
	return session.TelegramSessionID(userID)
}

func writeTelegramHeader(writer *bufio.Writer, colors simpleTheme, providerName string, width int) {
	title := "Benoit · " + providerName + " · Telegram"
	hint := "Listening for Telegram messages | Ctrl+C to quit"
	simpleui.WriteHeader(writer, colors, title, hint, width)
}

func writeTelegramIncoming(writer *bufio.Writer, colors simpleTheme, message channels.ChannelMessage) {
	if writer == nil {
		return
	}
	text := strings.TrimSpace(message.Text)
	if text == "" {
		text = "(empty message)"
	}
	header := formatTelegramIncomingHeader(message)
	fmt.Fprintln(writer, colors.Style(header, colors.Bold, colors.FGAccent))
	fmt.Fprintln(writer, colors.Style(text, colors.FGUser))
	fmt.Fprintln(writer)
}

func formatTelegramIncomingHeader(message channels.ChannelMessage) string {
	return simpleui.FormatTelegramIncomingHeader(message)
}

func runTelegramTypingLoop(ctx context.Context, telegramChannel channels.Channel, userID int64) {
	if telegramChannel == nil || userID == 0 {
		return
	}

	notify := func() bool {
		err := sendTelegramTypingSignal(ctx, telegramChannel, userID, true)
		if err == nil {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		fmt.Fprintln(os.Stderr, "telegram typing error:", err)
		return true
	}

	ticker := time.NewTicker(telegramTypingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !notify() {
				return
			}
		}
	}
}

func sendTelegramTypingSignal(ctx context.Context, telegramChannel channels.Channel, userID int64, typing bool) error {
	actionCtx := ctx
	cancel := func() {}
	if telegramTypingRequestTimeout > 0 {
		actionCtx, cancel = context.WithTimeout(ctx, telegramTypingRequestTimeout)
	}
	defer cancel()
	return telegramChannel.SendMessage(actionCtx, channels.ChannelMessage{UserID: userID, Type: channels.TypingEvent, Typing: typing})
}
