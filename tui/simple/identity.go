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
	name := strings.TrimSpace(ChannelMessageDisplayName(message))
	if name == "" {
		name = strings.TrimSpace(ChannelMessageUsername(message))
	}
	if name == "" {
		name = "unknown"
	}

	return fmt.Sprintf("%s (telegram:%d)", name, message.UserID)
}
