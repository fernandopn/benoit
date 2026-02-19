//go:build ignore

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/fernandopn/benoit/channels"
)

func main() {
	if err := runChannelTUI(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runChannelTUI(args []string) error {
	flagSet := flag.NewFlagSet("channels-tui", flag.ContinueOnError)
	action := flagSet.String("action", "send", "action: send or receive")
	messageType := flagSet.String("type", "message", "message type: message or typing")
	channelName := flagSet.String("channel", "telegram", "channel: telegram")
	telegramToken := flagSet.String("telegram-token", "", "telegram bot token")
	userID := flagSet.Int64("user", 0, "user/chat ID")
	pollTimeoutSeconds := flagSet.Int("poll-timeout", 30, "receive poll timeout in seconds")
	if err := flagSet.Parse(args); err != nil {
		return err
	}

	if strings.ToLower(strings.TrimSpace(*channelName)) != "telegram" {
		return fmt.Errorf("unsupported channel %q", *channelName)
	}

	telegramClient, err := channels.NewTelegram(strings.TrimSpace(*telegramToken), http.DefaultClient)
	if err != nil {
		return err
	}
	var channel channels.Channel = telegramClient

	switch strings.ToLower(strings.TrimSpace(*action)) {
	case "send":
		return runChannelSend(channel, strings.ToLower(strings.TrimSpace(*messageType)), *userID, flagSet.Args())
	case "receive":
		return runChannelReceive(channel, *pollTimeoutSeconds)
	default:
		return fmt.Errorf("unsupported action %q", *action)
	}
}

func runChannelSend(channel channels.Channel, messageType string, userID int64, positionalArgs []string) error {
	if userID == 0 {
		return errors.New("-user is required for send")
	}

	switch messageType {
	case "message":
		text := strings.TrimSpace(strings.Join(positionalArgs, " "))
		if text == "" {
			return errors.New("message positional argument is required for -type=message")
		}
		return channel.SendMessage(context.Background(), channels.ChannelMessage{Text: text, UserID: userID, Type: channels.TextMessage})
	case "typing":
		typing := true
		if len(positionalArgs) > 0 {
			parsedTyping, err := strconv.ParseBool(strings.TrimSpace(positionalArgs[0]))
			if err != nil {
				return errors.New("for -type=typing, optional positional argument must be true or false")
			}
			typing = parsedTyping
		}
		return channel.SendMessage(context.Background(), channels.ChannelMessage{UserID: userID, Type: channels.TypingEvent, Typing: typing})
	default:
		return fmt.Errorf("unsupported -type value %q (use message or typing)", messageType)
	}
}

func runChannelReceive(channel channels.Channel, pollTimeoutSeconds int) error {
	if pollTimeoutSeconds < 0 {
		return errors.New("-poll-timeout cannot be negative")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	incoming := make(chan channels.ChannelMessage)
	if err := channel.RegisterReceiveMessageChan(incoming); err != nil {
		return err
	}
	errs := make(chan error, 1)
	go func() {
		errs <- channel.Listen(ctx, pollTimeoutSeconds)
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errs:
			if err == nil || errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		case message := <-incoming:
			fmt.Printf("user=%d type=%s typing=%t text=%q\n", message.UserID, channelMessageTypeName(message.Type), message.Typing, message.Text)
		}
	}
}

func channelMessageTypeName(messageType channels.MessageType) string {
	switch messageType {
	case channels.TextMessage:
		return "message"
	case channels.TypingEvent:
		return "typing"
	default:
		return "unknown"
	}
}
