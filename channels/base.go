package channels

import "context"

type MessageType int

const (
	TextMessage MessageType = iota
	TypingEvent

	ParamUsername    = "username"
	ParamDisplayName = "display_name"
)

type ChannelMessage struct {
	Text   string
	UserID int64
	Type   MessageType
	Typing bool
	Params map[string]string
}

type Channel interface {
	SendMessage(ctx context.Context, message ChannelMessage) error
	RegisterReceiveMessageChan(receive chan<- ChannelMessage) error
	Listen(ctx context.Context, timeoutSeconds int) error
}
