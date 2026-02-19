package tui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fernandopn/benoid/channels"
	"github.com/fernandopn/benoid/providers"
	"golang.org/x/term"
)

const telegramMaxMessageLength = 4096
const telegramTypingInterval = 4 * time.Second
const telegramTypingRequestTimeout = 4 * time.Second

func RunTelegram(ctx context.Context, telegramClient *channels.Telegram, provider providers.Provider, timeout time.Duration, pollTimeoutSeconds int) error {
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

			reply, err := runTelegramPromptWithOutput(ctx, provider, prompt, timeout, writer, colors, width)
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
	return runTelegramPromptWithOutput(ctx, provider, prompt, timeout, nil, simpleTheme{}, 0)
}

func runTelegramPromptWithOutput(ctx context.Context, provider providers.Provider, prompt string, timeout time.Duration, writer *bufio.Writer, colors simpleTheme, width int) (string, error) {
	requestCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	msgs := provider.Chat(requestCtx, prompt)
	var (
		aggregated strings.Builder
		chatErr    error
		section    *providers.MsgType
		pending    = map[string]pendingToolCall{}
	)

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

	for msg := range msgs {
		switch msg.Type {
		case providers.MsgTypeChat:
			switchState(msg.Type)
			if writer != nil {
				fmt.Fprint(writer, colors.style(msg.Value, colors.fgStrong))
				writer.Flush()
			}
			aggregated.WriteString(msg.Value)
		case providers.MsgTypeReasoningSummary:
			switchState(msg.Type)
			if writer != nil {
				fmt.Fprint(writer, colors.style(msg.Value, colors.fgMuted, colors.dim))
				writer.Flush()
			}
		case providers.MsgTypeToolCall:
			switchState(providers.MsgTypeToolCall)
			if writer != nil {
				callID := strings.TrimSpace(msg.Metadata["call_id"])
				name := strings.TrimSpace(msg.Metadata["tool"])
				args := compactWhitespace(strings.TrimSpace(msg.Value))
				if args == "" {
					args = "{}"
				}
				if callID != "" {
					pending[callID] = pendingToolCall{name: name, args: args}
				}
				writeSimpleToolCard(writer, colors, width, name, args, "Running...")
				writer.Flush()
			}
		case providers.MsgTypeToolResult:
			switchState(providers.MsgTypeToolResult)
			if writer != nil {
				callID := strings.TrimSpace(msg.Metadata["call_id"])
				name := strings.TrimSpace(msg.Metadata["tool"])
				args := "{}"
				if callID != "" {
					if call, ok := pending[callID]; ok {
						if name == "" {
							name = call.name
						}
						args = call.args
						delete(pending, callID)
					}
				}
				if args == "{}" {
					if rawArgs := strings.TrimSpace(msg.Metadata["args"]); rawArgs != "" {
						args = compactWhitespace(rawArgs)
					}
				}
				output := strings.TrimSpace(msg.Value)
				if output == "" {
					output = "(empty output)"
				}
				writeSimpleToolCard(writer, colors, width, name, args, output)
				writer.Flush()
			}
		case providers.MsgTypeContextUsage:
			switchState(providers.MsgTypeContextUsage)
			if writer != nil {
				if left, ok := contextLeftPercent(msg.Value, msg.Metadata); ok {
					fmt.Fprintln(writer, colors.style(formatContextLeft(left), colors.fgAccent, colors.dim))
					writer.Flush()
				}
			}
		case providers.MsgTypeError:
			if chatErr == nil {
				errText := strings.TrimSpace(msg.Value)
				if errText == "" {
					errText = "provider returned an empty error"
				}
				chatErr = errors.New(errText)
			}
			if writer != nil {
				fmt.Fprintln(os.Stderr, colors.style("request error:", colors.bold, colors.fgWarn), msg.Value)
			}
		}
	}
	if writer != nil && section != nil {
		fmt.Fprintln(writer)
		writer.Flush()
	}
	if chatErr != nil {
		return "", chatErr
	}
	if err := requestCtx.Err(); err != nil {
		return "", err
	}
	reply := aggregated.String()
	if strings.TrimSpace(reply) == "" {
		return "(empty response)", nil
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

func writeTelegramHeader(writer *bufio.Writer, colors simpleTheme, providerName string, width int) {
	left := "Benoid · " + providerName + " · Telegram"
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
	chat := telegramChatLabel(message)
	text := strings.TrimSpace(message.Text)
	if text == "" {
		text = "(empty message)"
	}
	header := "from " + sender
	if senderID != "" {
		header += " (id:" + senderID + ")"
	}
	if chat != "" {
		header += " in " + chat
	}
	fmt.Fprintln(writer, colors.style(header+":", colors.bold, colors.fgAccent))
	fmt.Fprintln(writer, colors.style(text, colors.fgUser))
	fmt.Fprintln(writer)
}

func telegramSenderLabel(message *channels.TelegramMessage) string {
	if message == nil {
		return "unknown sender"
	}
	if message.From != nil {
		if username := strings.TrimSpace(message.From.Username); username != "" {
			return "@" + username
		}
		fullName := strings.TrimSpace(strings.TrimSpace(message.From.FirstName) + " " + strings.TrimSpace(message.From.LastName))
		if fullName != "" {
			return fullName
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

func telegramChatLabel(message *channels.TelegramMessage) string {
	if message == nil {
		return ""
	}
	if username := strings.TrimSpace(message.Chat.Username); username != "" {
		return "@" + username
	}
	if title := strings.TrimSpace(message.Chat.Title); title != "" {
		return title
	}
	fullName := strings.TrimSpace(strings.TrimSpace(message.Chat.FirstName) + " " + strings.TrimSpace(message.Chat.LastName))
	if fullName != "" {
		return fullName
	}
	if message.Chat.ID != 0 {
		return fmt.Sprintf("chat:%d", message.Chat.ID)
	}
	return ""
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
