package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const TelegramAPIBaseURL = "https://api.telegram.org"

type Telegram struct {
	botToken   string
	apiBaseURL string
	httpClient *http.Client

	receiveMu    sync.RWMutex
	receiveChans []chan<- ChannelMessage
}

var _ Channel = (*Telegram)(nil)

type TelegramUser struct {
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
}

type TelegramChat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type TelegramMessage struct {
	ID              int64         `json:"message_id"`
	Date            int64         `json:"date"`
	Text            string        `json:"text"`
	Chat            TelegramChat  `json:"chat"`
	From            *TelegramUser `json:"from,omitempty"`
	SenderChat      *TelegramChat `json:"sender_chat,omitempty"`
	AuthorSignature string        `json:"author_signature,omitempty"`
}

type TelegramUpdate struct {
	ID            int64            `json:"update_id"`
	Message       *TelegramMessage `json:"message,omitempty"`
	EditedMessage *TelegramMessage `json:"edited_message,omitempty"`
	ChannelPost   *TelegramMessage `json:"channel_post,omitempty"`
}

type TelegramAPIError struct {
	Method      string
	StatusCode  int
	ErrorCode   int
	Description string
}

type telegramEnvelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
}

type sendMessageRequest struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
}

type sendChatActionRequest struct {
	ChatID int64  `json:"chat_id"`
	Action string `json:"action"`
}

type getUpdatesRequest struct {
	Offset  int64 `json:"offset"`
	Timeout int   `json:"timeout"`
}

func NewTelegram(botToken string, httpClient *http.Client) (*Telegram, error) {
	return NewTelegramWithBaseURL(botToken, TelegramAPIBaseURL, httpClient)
}

func NewTelegramWithBaseURL(botToken string, apiBaseURL string, httpClient *http.Client) (*Telegram, error) {
	botToken = strings.TrimSpace(botToken)
	if botToken == "" {
		return nil, errors.New("telegram bot token is required")
	}
	apiBaseURL = strings.TrimSpace(apiBaseURL)
	if apiBaseURL == "" {
		return nil, errors.New("telegram API base URL is required")
	}
	if _, err := url.ParseRequestURI(apiBaseURL); err != nil {
		return nil, fmt.Errorf("invalid telegram API base URL: %w", err)
	}
	if httpClient == nil {
		return nil, errors.New("http client is required")
	}
	return &Telegram{botToken: botToken, apiBaseURL: strings.TrimRight(apiBaseURL, "/"), httpClient: httpClient}, nil
}

func (e *TelegramAPIError) Error() string {
	if e == nil {
		return ""
	}
	msg := fmt.Sprintf("telegram API %s failed", strings.TrimSpace(e.Method))
	if e.ErrorCode != 0 {
		msg += fmt.Sprintf(" (error_code=%d)", e.ErrorCode)
	} else if e.StatusCode != 0 {
		msg += fmt.Sprintf(" (status=%d)", e.StatusCode)
	}
	if description := strings.TrimSpace(e.Description); description != "" {
		msg += ": " + description
	}
	return msg
}

func (t *Telegram) SendMessage(ctx context.Context, message ChannelMessage) error {
	if t == nil {
		return errors.New("telegram client is required")
	}
	if message.UserID == 0 {
		return errors.New("user ID is required")
	}

	switch message.Type {
	case TextMessage:
		if strings.TrimSpace(message.Text) == "" {
			return errors.New("message text is required")
		}
		return t.sendTextMessage(ctx, message.UserID, message.Text)
	case TypingEvent:
		return t.sendTypingEvent(ctx, message.UserID, message.Typing)
	default:
		return errors.New("message type is required")
	}
}

func (t *Telegram) RegisterReceiveMessageChan(receive chan<- ChannelMessage) error {
	if t == nil {
		return errors.New("telegram client is required")
	}
	if receive == nil {
		return errors.New("receive channel is required")
	}
	t.receiveMu.Lock()
	t.receiveChans = append(t.receiveChans, receive)
	t.receiveMu.Unlock()
	return nil
}

func (t *Telegram) Listen(ctx context.Context, timeoutSeconds int) error {
	if t == nil {
		return errors.New("telegram client is required")
	}
	if timeoutSeconds < 0 {
		return errors.New("timeout seconds cannot be negative")
	}

	offset := int64(0)
	for {
		updates, err := t.ReceiveMessages(ctx, offset, timeoutSeconds)
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
			channelMessage, ok := toChannelMessage(incoming)
			if !ok {
				continue
			}
			if err := t.broadcast(ctx, channelMessage); err != nil {
				return err
			}
		}
	}
}

func (t *Telegram) broadcast(ctx context.Context, message ChannelMessage) error {
	receiveChans := t.snapshotReceiveChans()
	for _, receive := range receiveChans {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case receive <- message:
		}
	}
	return nil
}

func (t *Telegram) snapshotReceiveChans() []chan<- ChannelMessage {
	t.receiveMu.RLock()
	defer t.receiveMu.RUnlock()
	if len(t.receiveChans) == 0 {
		return nil
	}
	receiveChans := make([]chan<- ChannelMessage, len(t.receiveChans))
	copy(receiveChans, t.receiveChans)
	return receiveChans
}

func (t *Telegram) ReceiveMessages(ctx context.Context, offset int64, timeoutSeconds int) ([]TelegramUpdate, error) {
	if t == nil {
		return nil, errors.New("telegram client is required")
	}
	if timeoutSeconds < 0 {
		return nil, errors.New("timeout seconds cannot be negative")
	}

	req := getUpdatesRequest{Offset: offset, Timeout: timeoutSeconds}
	updates := make([]TelegramUpdate, 0)
	if err := t.do(ctx, "getUpdates", req, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

func (t *Telegram) SendTyping(ctx context.Context, chatID int64) error {
	return t.sendTypingEvent(ctx, chatID, true)
}

func (t *Telegram) sendTextMessage(ctx context.Context, userID int64, text string) error {
	req := sendMessageRequest{ChatID: userID, Text: text}
	var response TelegramMessage
	if err := t.do(ctx, "sendMessage", req, &response); err != nil {
		return err
	}
	return nil
}

func (t *Telegram) sendTypingEvent(ctx context.Context, userID int64, typing bool) error {
	if t == nil {
		return errors.New("telegram client is required")
	}
	if userID == 0 {
		return errors.New("user ID is required")
	}
	if !typing {
		return nil
	}

	req := sendChatActionRequest{ChatID: userID, Action: "typing"}
	if err := t.do(ctx, "sendChatAction", req, nil); err != nil {
		return err
	}
	return nil
}

func incomingTelegramMessage(update TelegramUpdate) *TelegramMessage {
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

func toChannelMessage(message *TelegramMessage) (ChannelMessage, bool) {
	if message == nil {
		return ChannelMessage{}, false
	}
	if message.From != nil && message.From.IsBot {
		return ChannelMessage{}, false
	}
	if strings.TrimSpace(message.Text) == "" {
		return ChannelMessage{}, false
	}

	userID := int64(0)
	if message.From != nil {
		userID = message.From.ID
	}
	if userID == 0 {
		userID = message.Chat.ID
	}
	if userID == 0 {
		return ChannelMessage{}, false
	}

	return ChannelMessage{Text: message.Text, UserID: userID, Type: TextMessage}, true
}

func (t *Telegram) do(ctx context.Context, method string, request any, result any) error {
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}

	endpoint := t.apiBaseURL + "/bot" + t.botToken + "/" + strings.TrimSpace(method)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	rawBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return err
	}

	var envelope telegramEnvelope
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return fmt.Errorf("decode telegram response: %w", err)
	}

	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices || !envelope.OK {
		description := strings.TrimSpace(envelope.Description)
		if description == "" {
			description = strings.TrimSpace(string(rawBody))
		}
		return &TelegramAPIError{
			Method:      method,
			StatusCode:  httpResp.StatusCode,
			ErrorCode:   envelope.ErrorCode,
			Description: description,
		}
	}

	if result == nil {
		return nil
	}
	if len(envelope.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return fmt.Errorf("decode telegram result: %w", err)
	}
	return nil
}
