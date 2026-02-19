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

const (
	telegramMethodGetUpdates     = "getUpdates"
	telegramMethodSendMessage    = "sendMessage"
	telegramMethodSendChatAction = "sendChatAction"
	telegramChatActionTyping     = "typing"
)

var (
	errTelegramClientRequired = errors.New("telegram client is required")
	errBotTokenRequired       = errors.New("telegram bot token is required")
	errAPIBaseURLRequired     = errors.New("telegram API base URL is required")
	errHTTPClientRequired     = errors.New("http client is required")
	errUserIDRequired         = errors.New("user ID is required")
	errMessageTextRequired    = errors.New("message text is required")
	errMessageTypeRequired    = errors.New("message type is required")
	errReceiveChannelRequired = errors.New("receive channel is required")
	errNegativeTimeoutSeconds = errors.New("timeout seconds cannot be negative")
	errTelegramMethodRequired = errors.New("telegram method is required")
)

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
		return nil, errBotTokenRequired
	}

	apiBaseURL = strings.TrimSpace(apiBaseURL)
	if apiBaseURL == "" {
		return nil, errAPIBaseURLRequired
	}
	if _, err := url.ParseRequestURI(apiBaseURL); err != nil {
		return nil, fmt.Errorf("invalid telegram API base URL: %w", err)
	}

	if httpClient == nil {
		return nil, errHTTPClientRequired
	}

	return &Telegram{
		botToken:   botToken,
		apiBaseURL: strings.TrimRight(apiBaseURL, "/"),
		httpClient: httpClient,
	}, nil
}

func (e *TelegramAPIError) Error() string {
	if e == nil {
		return ""
	}

	message := fmt.Sprintf("telegram API %s failed", strings.TrimSpace(e.Method))
	if e.ErrorCode != 0 {
		message += fmt.Sprintf(" (error_code=%d)", e.ErrorCode)
	} else if e.StatusCode != 0 {
		message += fmt.Sprintf(" (status=%d)", e.StatusCode)
	}
	if description := strings.TrimSpace(e.Description); description != "" {
		message += ": " + description
	}

	return message
}

func (t *Telegram) SendMessage(ctx context.Context, message ChannelMessage) error {
	if t == nil {
		return errTelegramClientRequired
	}
	if message.UserID == 0 {
		return errUserIDRequired
	}

	switch message.Type {
	case TextMessage:
		return t.sendTextMessage(ctx, message.UserID, message.Text)
	case TypingEvent:
		return t.sendTypingEvent(ctx, message.UserID, message.Typing)
	default:
		return errMessageTypeRequired
	}
}

func (t *Telegram) RegisterReceiveMessageChan(receive chan<- ChannelMessage) error {
	if t == nil {
		return errTelegramClientRequired
	}
	if receive == nil {
		return errReceiveChannelRequired
	}

	t.receiveMu.Lock()
	t.receiveChans = append(t.receiveChans, receive)
	t.receiveMu.Unlock()

	return nil
}

func (t *Telegram) Listen(ctx context.Context, timeoutSeconds int) error {
	if t == nil {
		return errTelegramClientRequired
	}
	if timeoutSeconds < 0 {
		return errNegativeTimeoutSeconds
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
			if nextOffset := update.ID + 1; nextOffset > offset {
				offset = nextOffset
			}

			channelMessage, ok := toChannelMessage(incomingTelegramMessage(update))
			if !ok {
				continue
			}

			if err := t.broadcast(ctx, channelMessage); err != nil {
				return err
			}
		}
	}
}

func (t *Telegram) ReceiveMessages(ctx context.Context, offset int64, timeoutSeconds int) ([]TelegramUpdate, error) {
	if t == nil {
		return nil, errTelegramClientRequired
	}
	if timeoutSeconds < 0 {
		return nil, errNegativeTimeoutSeconds
	}

	request := getUpdatesRequest{Offset: offset, Timeout: timeoutSeconds}
	updates := make([]TelegramUpdate, 0)
	if err := t.do(ctx, telegramMethodGetUpdates, request, &updates); err != nil {
		return nil, err
	}

	return updates, nil
}

func (t *Telegram) SendTyping(ctx context.Context, chatID int64) error {
	return t.sendTypingEvent(ctx, chatID, true)
}

func (t *Telegram) sendTextMessage(ctx context.Context, userID int64, text string) error {
	if t == nil {
		return errTelegramClientRequired
	}
	if userID == 0 {
		return errUserIDRequired
	}
	if strings.TrimSpace(text) == "" {
		return errMessageTextRequired
	}

	request := sendMessageRequest{ChatID: userID, Text: text}
	var response TelegramMessage
	if err := t.do(ctx, telegramMethodSendMessage, request, &response); err != nil {
		return err
	}

	return nil
}

func (t *Telegram) sendTypingEvent(ctx context.Context, userID int64, typing bool) error {
	if t == nil {
		return errTelegramClientRequired
	}
	if userID == 0 {
		return errUserIDRequired
	}
	if !typing {
		return nil
	}

	request := sendChatActionRequest{ChatID: userID, Action: telegramChatActionTyping}
	if err := t.do(ctx, telegramMethodSendChatAction, request, nil); err != nil {
		return err
	}

	return nil
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

	return ChannelMessage{
		Text:   message.Text,
		UserID: userID,
		Type:   TextMessage,
		Params: telegramMessageParams(message),
	}, true
}

func telegramMessageParams(message *TelegramMessage) map[string]string {
	if message == nil {
		return nil
	}

	username := telegramMessageUsername(message)
	displayName := telegramMessageDisplayName(message)
	if username == "" && displayName == "" {
		return nil
	}

	params := make(map[string]string, 2)
	if username != "" {
		params[ParamUsername] = username
	}
	if displayName != "" {
		params[ParamDisplayName] = displayName
	}
	return params
}

func telegramMessageUsername(message *TelegramMessage) string {
	if message == nil {
		return ""
	}

	if message.From != nil {
		if username := normalizeTelegramUsername(message.From.Username); username != "" {
			return username
		}
	}

	if username := normalizeTelegramUsername(message.Chat.Username); username != "" {
		return username
	}

	return ""
}

func telegramMessageDisplayName(message *TelegramMessage) string {
	if message == nil {
		return ""
	}

	if message.From != nil {
		if personName := telegramPersonName(message.From.FirstName, message.From.LastName); personName != "" {
			return personName
		}
	}

	if message.SenderChat != nil {
		if title := strings.TrimSpace(message.SenderChat.Title); title != "" {
			return title
		}
		if username := normalizeTelegramUsername(message.SenderChat.Username); username != "" {
			return "@" + username
		}
	}

	if personName := telegramPersonName(message.Chat.FirstName, message.Chat.LastName); personName != "" {
		return personName
	}
	if title := strings.TrimSpace(message.Chat.Title); title != "" {
		return title
	}
	if username := telegramMessageUsername(message); username != "" {
		return "@" + username
	}

	return ""
}

func telegramPersonName(firstName string, lastName string) string {
	firstName = strings.TrimSpace(firstName)
	lastName = strings.TrimSpace(lastName)
	return strings.TrimSpace(firstName + " " + lastName)
}

func normalizeTelegramUsername(username string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	return strings.TrimPrefix(username, "@")
}

func (t *Telegram) do(ctx context.Context, method string, request any, result any) error {
	if t == nil {
		return errTelegramClientRequired
	}

	method = strings.TrimSpace(method)
	if method == "" {
		return errTelegramMethodRequired
	}

	body, err := json.Marshal(request)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.methodURL(method), bytes.NewReader(body))
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

	envelope, err := decodeTelegramEnvelope(rawBody)
	if err != nil {
		if !httpStatusOK(httpResp.StatusCode) {
			return &TelegramAPIError{
				Method:      method,
				StatusCode:  httpResp.StatusCode,
				Description: strings.TrimSpace(string(rawBody)),
			}
		}
		return fmt.Errorf("decode telegram response: %w", err)
	}

	if apiErr := telegramResponseError(method, httpResp.StatusCode, envelope, rawBody); apiErr != nil {
		return apiErr
	}

	if result == nil || len(envelope.Result) == 0 {
		return nil
	}

	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return fmt.Errorf("decode telegram result: %w", err)
	}

	return nil
}

func (t *Telegram) methodURL(method string) string {
	return t.apiBaseURL + "/bot" + t.botToken + "/" + method
}

func decodeTelegramEnvelope(rawBody []byte) (telegramEnvelope, error) {
	var envelope telegramEnvelope
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return telegramEnvelope{}, err
	}
	return envelope, nil
}

func telegramResponseError(method string, statusCode int, envelope telegramEnvelope, rawBody []byte) error {
	if httpStatusOK(statusCode) && envelope.OK {
		return nil
	}

	description := strings.TrimSpace(envelope.Description)
	if description == "" {
		description = strings.TrimSpace(string(rawBody))
	}

	return &TelegramAPIError{
		Method:      method,
		StatusCode:  statusCode,
		ErrorCode:   envelope.ErrorCode,
		Description: description,
	}
}

func httpStatusOK(statusCode int) bool {
	return statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
}
