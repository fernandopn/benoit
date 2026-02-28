package simple

import (
	"testing"

	"github.com/fernandopn/benoit/channels"
)

func TestFormatTelegramIncomingHeader(t *testing.T) {
	message := channels.ChannelMessage{
		UserID: 8230557735,
		Params: map[string]string{
			channels.ParamUsername:    "telegram_user",
			channels.ParamDisplayName: "Telegram User",
		},
	}

	if got := FormatTelegramIncomingHeader(message); got != "Telegram User (telegram:8230557735)" {
		t.Fatalf("unexpected header: %q", got)
	}
}

func TestFormatTelegramIncomingHeaderWithoutUsername(t *testing.T) {
	message := channels.ChannelMessage{
		UserID: 8230557735,
		Params: map[string]string{
			channels.ParamDisplayName: "Telegram User",
		},
	}

	if got := FormatTelegramIncomingHeader(message); got != "Telegram User (telegram:8230557735)" {
		t.Fatalf("unexpected header: %q", got)
	}
}

func TestFormatTelegramIncomingHeaderWithoutDisplayName(t *testing.T) {
	message := channels.ChannelMessage{
		UserID: 8230557735,
		Params: map[string]string{
			channels.ParamUsername: "telegram_user",
		},
	}

	if got := FormatTelegramIncomingHeader(message); got != "telegram_user (telegram:8230557735)" {
		t.Fatalf("unexpected header: %q", got)
	}
}
