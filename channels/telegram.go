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
	"os"
	"strings"
)

const TelegramAPIBaseURL = "https://api.telegram.org"
const TelegramAPIKeyEnv = "TELEGRAM_API_KEY"

type Telegram struct {
	botToken   string
	apiBaseURL string
	httpClient *http.Client
}

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

func NewTelegramFromEnv(httpClient *http.Client) (*Telegram, error) {
	botToken := strings.TrimSpace(os.Getenv(TelegramAPIKeyEnv))
	if botToken == "" {
		return nil, fmt.Errorf("%s is not set", TelegramAPIKeyEnv)
	}
	return NewTelegram(botToken, httpClient)
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

func (t *Telegram) SendMessage(ctx context.Context, chatID int64, text string) (*TelegramMessage, error) {
	if t == nil {
		return nil, errors.New("telegram client is required")
	}
	if chatID == 0 {
		return nil, errors.New("chat ID is required")
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("message text is required")
	}

	req := sendMessageRequest{ChatID: chatID, Text: text}
	var message TelegramMessage
	if err := t.do(ctx, "sendMessage", req, &message); err != nil {
		return nil, err
	}
	return &message, nil
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
	if t == nil {
		return errors.New("telegram client is required")
	}
	if chatID == 0 {
		return errors.New("chat ID is required")
	}

	req := sendChatActionRequest{ChatID: chatID, Action: "typing"}
	if err := t.do(ctx, "sendChatAction", req, nil); err != nil {
		return err
	}
	return nil
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
