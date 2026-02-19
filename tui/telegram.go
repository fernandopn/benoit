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
	"golang.org/x/term"
)

const telegramMaxMessageLength = 4096
const telegramTypingInterval = 4 * time.Second
const telegramTypingRequestTimeout = 4 * time.Second

func RunTelegram(ctx context.Context, telegramClient *channels.Telegram, provider providers.Provider, timeout time.Duration, pollTimeoutSeconds int, allowedUserIDs []int64) error {
	if telegramClient == nil {
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

	offset := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		updates, err := telegramClient.ReceiveMessages(ctx, offset, pollTimeoutSeconds)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return err
		}

		for _, update := range updates {
			nextOffset := update.ID + 1
			if nextOffset > offset {
				offset = nextOffset
			}

			incoming := incomingTelegramMessage(update)
			if incoming == nil {
				continue
			}
			if incoming.From != nil && incoming.From.IsBot {
				continue
			}
			if !isTelegramUserAllowed(incoming, allowedUsers) {
				continue
			}
			prompt := strings.TrimSpace(incoming.Text)
			if prompt == "" {
				continue
			}

			writeTelegramIncoming(writer, colors, incoming)
			writer.Flush()
			if err := sendTelegramTypingSignal(ctx, telegramClient, incoming.Chat.ID); err != nil && ctx.Err() == nil {
				fmt.Fprintln(os.Stderr, "telegram typing error:", err)
			}

			typingCtx, stopTyping := context.WithCancel(ctx)
			typingDone := make(chan struct{})
			go func() {
				runTelegramTypingLoop(typingCtx, telegramClient, incoming.Chat.ID)
				close(typingDone)
			}()

			sessionID := telegramSessionID(incoming)
			reply, err := runTelegramPromptWithOutput(ctx, provider, prompt, timeout, sessionID, writer, colors, width)
			stopTyping()
			<-typingDone
			if err != nil {
				reply = "request error: " + err.Error()
			}

			if err := sendTelegramReply(ctx, telegramClient, incoming.Chat.ID, reply); err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				return err
			}

			fmt.Fprintln(writer, colors.style("sent reply to Telegram", colors.fgMuted, colors.dim))
			fmt.Fprintln(writer)
			writer.Flush()
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

func isTelegramUserAllowed(message *channels.TelegramMessage, allowed map[int64]struct{}) bool {
	if len(allowed) == 0 {
		return true
	}
	if message == nil || message.From == nil || message.From.ID == 0 {
		return false
	}
	_, ok := allowed[message.From.ID]
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
			switchState(providers.MsgTypeChat)
			if writer != nil {
				fmt.Fprint(writer, colors.style(value, colors.fgStrong))
				writer.Flush()
			}
		},
		OnReasoning: func(value string) {
			switchState(providers.MsgTypeReasoningSummary)
			if writer != nil {
				fmt.Fprint(writer, colors.style(value, colors.fgMuted, colors.dim))
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
				if left, ok := contextLeftPercent(value, metadata); ok {
					fmt.Fprintln(writer, colors.style(formatContextLeft(left), colors.fgAccent, colors.dim))
					writer.Flush()
				}
			}
		},
		OnError: func(errText string) {
			if writer != nil {
				fmt.Fprintln(os.Stderr, colors.style("request error:", colors.bold, colors.fgWarn), errText)
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

func sendTelegramReply(ctx context.Context, telegramClient *channels.Telegram, chatID int64, text string) error {
	chunks := splitTelegramMessage(text, telegramMaxMessageLength)
	for _, chunk := range chunks {
		if _, err := telegramClient.SendMessage(ctx, chatID, chunk); err != nil {
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

func incomingTelegramMessage(update channels.TelegramUpdate) *channels.TelegramMessage {
	if update.Message != nil {
		return update.Message
	}
	if update.EditedMessage != nil {
		return update.EditedMessage
	}
	if update.ChannelPost != nil {
		return update.ChannelPost
	}
	return nil
}

func telegramSessionID(message *channels.TelegramMessage) string {
	if message == nil || message.Chat.ID == 0 {
		return ""
	}
	return fmt.Sprintf("telegram:%d", message.Chat.ID)
}

func writeTelegramHeader(writer *bufio.Writer, colors simpleTheme, providerName string, width int) {
	left := "Benoit · " + providerName + " · Telegram"
	fmt.Fprintln(writer, colors.style(left, colors.bold, colors.fgAccent))
	if width > 0 {
		hint := "Listening for Telegram messages | Ctrl+C to quit"
		fmt.Fprintln(writer, colors.style(hint, colors.dim, colors.fgMuted))
	}
	fmt.Fprintln(writer)
}

func writeTelegramIncoming(writer *bufio.Writer, colors simpleTheme, message *channels.TelegramMessage) {
	if writer == nil || message == nil {
		return
	}
	sender := telegramSenderLabel(message)
	senderID := telegramSenderID(message)
	text := strings.TrimSpace(message.Text)
	if text == "" {
		text = "(empty message)"
	}
	header := sender
	if senderID != "" {
		header += " (id:" + senderID + ")"
	}
	fmt.Fprintln(writer, colors.style(header, colors.bold, colors.fgAccent))
	fmt.Fprintln(writer, colors.style(text, colors.fgUser))
	fmt.Fprintln(writer)
}

func telegramSenderLabel(message *channels.TelegramMessage) string {
	if message == nil {
		return "unknown sender"
	}
	if message.From != nil {
		fullName := strings.TrimSpace(strings.TrimSpace(message.From.FirstName) + " " + strings.TrimSpace(message.From.LastName))
		if fullName != "" {
			return fullName
		}
		if username := strings.TrimSpace(message.From.Username); username != "" {
			return "@" + username
		}
		if message.From.ID != 0 {
			return fmt.Sprintf("user:%d", message.From.ID)
		}
	}
	if message.SenderChat != nil {
		if username := strings.TrimSpace(message.SenderChat.Username); username != "" {
			return "@" + username
		}
		if title := strings.TrimSpace(message.SenderChat.Title); title != "" {
			return title
		}
		if message.SenderChat.ID != 0 {
			return fmt.Sprintf("chat:%d", message.SenderChat.ID)
		}
	}
	if signature := strings.TrimSpace(message.AuthorSignature); signature != "" {
		return signature
	}
	return "unknown sender"
}

func telegramSenderID(message *channels.TelegramMessage) string {
	if message == nil || message.From == nil || message.From.ID == 0 {
		return ""
	}
	return fmt.Sprintf("%d", message.From.ID)
}

func runTelegramTypingLoop(ctx context.Context, telegramClient *channels.Telegram, chatID int64) {
	if telegramClient == nil || chatID == 0 {
		return
	}

	notify := func() bool {
		err := sendTelegramTypingSignal(ctx, telegramClient, chatID)
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

func sendTelegramTypingSignal(ctx context.Context, telegramClient *channels.Telegram, chatID int64) error {
	actionCtx := ctx
	cancel := func() {}
	if telegramTypingRequestTimeout > 0 {
		actionCtx, cancel = context.WithTimeout(ctx, telegramTypingRequestTimeout)
	}
	defer cancel()
	return telegramClient.SendTyping(actionCtx, chatID)
}
