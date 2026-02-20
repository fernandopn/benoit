package simple

import (
	"fmt"
	"strings"

	"github.com/fernandopn/benoit/channels"
)

func NormalizeUsername(username string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	return strings.TrimPrefix(username, "@")
}

func ChannelMessageUsername(message channels.ChannelMessage) string {
	if len(message.Params) == 0 {
		return ""
	}
	return NormalizeUsername(message.Params[channels.ParamUsername])
}

func ChannelMessageDisplayName(message channels.ChannelMessage) string {
	if len(message.Params) == 0 {
		return ""
	}
	return strings.TrimSpace(message.Params[channels.ParamDisplayName])
}

func FormatTelegramIncomingHeader(message channels.ChannelMessage) string {
	if message.UserID == 0 {
		return "unknown sender"
	}

	header := fmt.Sprintf("user:%d", message.UserID)
	username := ChannelMessageUsername(message)
	displayName := ChannelMessageDisplayName(message)
	if username != "" {
		header += fmt.Sprintf(" <%s>", username)
	}
	if displayName != "" {
		header += fmt.Sprintf(" (%s)", displayName)
	}

	return header
}
