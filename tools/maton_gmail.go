package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

const matonGoogleMailApp = "google-mail"

// MatonGmailTool integrates Gmail through Maton managed OAuth.
type MatonGmailTool struct {
	client     *MatonClient
	httpClient httpDoer
}

func NewMatonGmailTool() *MatonGmailTool {
	return NewMatonGmailToolWithHTTPClient(http.DefaultClient)
}

func NewMatonGmailToolWithHTTPClient(httpClient httpDoer) *MatonGmailTool {
	return &MatonGmailTool{httpClient: httpClient}
}

func NewMatonGmailToolWithClient(client *MatonClient) *MatonGmailTool {
	return &MatonGmailTool{client: client}
}

func (m *MatonGmailTool) Name() string {
	return "maton_gmail"
}

func (m *MatonGmailTool) Definition() responses.ToolUnionParam {
	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name:        m.Name(),
			Description: openai.String("Gmail via Maton. Actions: list/get messages, send/modify/trash messages, list/get threads, list labels, create/send drafts, get profile, and connection management. For send_message, prefer message.raw as base64url RFC822 with no whitespace. Convenience mode is also supported with message.to/message.subject/message.body (plus optional cc/bcc/content_type), and the tool will generate message.raw."),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type": "string",
						"enum": []string{
							"list_messages",
							"get_message",
							"send_message",
							"list_labels",
							"list_threads",
							"get_thread",
							"modify_message_labels",
							"trash_message",
							"create_draft",
							"send_draft",
							"get_profile",
							"list_connections",
							"create_connection",
							"get_connection",
							"delete_connection",
						},
						"description": "Operation to perform",
					},
					"message_id": map[string]any{
						"type":        "string",
						"description": "Gmail message ID",
					},
					"thread_id": map[string]any{
						"type":        "string",
						"description": "Gmail thread ID",
					},
					"draft_id": map[string]any{
						"type":        "string",
						"description": "Gmail draft ID",
					},
					"connection_id": map[string]any{
						"type":        "string",
						"description": "Maton connection ID (also used as Maton-Connection on gateway requests)",
					},
					"query": map[string]any{
						"type":                 "object",
						"description":          "Query parameters for the selected action",
						"additionalProperties": true,
					},
					"message": map[string]any{
						"type":                 "object",
						"description":          "Message payload for send_message. Preferred: {raw: base64url RFC822 string, no whitespace}. Convenience also supported: {to, subject, body/text/html, cc?, bcc?, content_type?}; tool builds raw.",
						"additionalProperties": true,
					},
					"modify": map[string]any{
						"type":                 "object",
						"description":          "Modify payload (for modify_message_labels)",
						"additionalProperties": true,
					},
					"draft": map[string]any{
						"type":                 "object",
						"description":          "Draft payload (for create_draft)",
						"additionalProperties": true,
					},
					"draft_send": map[string]any{
						"type":                 "object",
						"description":          "Draft send payload (optional for send_draft)",
						"additionalProperties": true,
					},
					"metadata": map[string]any{
						"type":                 "object",
						"description":          "Optional metadata for create_connection",
						"additionalProperties": true,
					},
				},
				"required":             []string{"action"},
				"additionalProperties": false,
			},
			Strict: openai.Bool(false),
		},
	}
}

func (m *MatonGmailTool) Call(ctx context.Context, args map[string]any) (string, error) {
	action, err := requireStringArg(args, "action")
	if err != nil {
		return toolError(err), nil
	}
	action = strings.ToLower(strings.TrimSpace(action))

	client, err := m.resolveClient()
	if err != nil {
		return toolError(err), nil
	}

	query := map[string]string(nil)
	if rawQuery, ok, err := optionalObjectArg(args, "query"); err != nil {
		return toolError(err), nil
	} else if ok {
		query, err = objectToQuery(rawQuery)
		if err != nil {
			return toolError(err), nil
		}
	}

	connectionID, _, err := optionalStringArg(args, "connection_id")
	if err != nil {
		return toolError(err), nil
	}

	var payload []byte
	switch action {
	case "list_messages":
		payload, err = client.GatewayJSON(ctx, http.MethodGet, m.gmailPath("messages"), query, nil, connectionID)
	case "get_message":
		messageID, argErr := requireStringArg(args, "message_id")
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodGet, m.gmailPath("messages", url.PathEscape(messageID)), query, nil, connectionID)
	case "send_message":
		messageBody, ok, argErr := optionalObjectArg(args, "message")
		if argErr != nil {
			return toolError(argErr), nil
		}
		if !ok {
			messageBody = collectTopLevelMessageFields(args)
			if len(messageBody) == 0 {
				return toolError(fmt.Errorf("missing required argument: message")), nil
			}
		}
		sendBody, argErr := normalizeSendMessageBody(messageBody)
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodPost, m.gmailPath("messages", "send"), query, sendBody, connectionID)
	case "list_labels":
		payload, err = client.GatewayJSON(ctx, http.MethodGet, m.gmailPath("labels"), query, nil, connectionID)
	case "list_threads":
		payload, err = client.GatewayJSON(ctx, http.MethodGet, m.gmailPath("threads"), query, nil, connectionID)
	case "get_thread":
		threadID, argErr := requireStringArg(args, "thread_id")
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodGet, m.gmailPath("threads", url.PathEscape(threadID)), query, nil, connectionID)
	case "modify_message_labels":
		messageID, argErr := requireStringArg(args, "message_id")
		if argErr != nil {
			return toolError(argErr), nil
		}
		modifyBody, argErr := requireObjectArg(args, "modify")
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodPost, m.gmailPath("messages", url.PathEscape(messageID), "modify"), query, modifyBody, connectionID)
	case "trash_message":
		messageID, argErr := requireStringArg(args, "message_id")
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodPost, m.gmailPath("messages", url.PathEscape(messageID), "trash"), query, nil, connectionID)
	case "create_draft":
		draftBody, argErr := requireObjectArg(args, "draft")
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodPost, m.gmailPath("drafts"), query, draftBody, connectionID)
	case "send_draft":
		draftSendBody, ok, argErr := optionalObjectArg(args, "draft_send")
		if argErr != nil {
			return toolError(argErr), nil
		}
		if !ok {
			draftID, argErr := requireStringArg(args, "draft_id")
			if argErr != nil {
				return toolError(argErr), nil
			}
			draftSendBody = map[string]any{"id": draftID}
		}
		payload, err = client.GatewayJSON(ctx, http.MethodPost, m.gmailPath("drafts", "send"), query, draftSendBody, connectionID)
	case "get_profile":
		payload, err = client.GatewayJSON(ctx, http.MethodGet, m.gmailPath("profile"), query, nil, connectionID)
	case "list_connections":
		if query == nil {
			query = map[string]string{}
		}
		if _, ok := query["app"]; !ok {
			query["app"] = matonGoogleMailApp
		}
		payload, err = client.ControlJSON(ctx, http.MethodGet, "connections", query, nil)
	case "create_connection":
		body := map[string]any{"app": matonGoogleMailApp}
		if metadata, ok, argErr := optionalObjectArg(args, "metadata"); argErr != nil {
			return toolError(argErr), nil
		} else if ok {
			body["metadata"] = metadata
		}
		payload, err = client.ControlJSON(ctx, http.MethodPost, "connections", query, body)
	case "get_connection":
		targetConnectionID, argErr := requireStringArg(args, "connection_id")
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.ControlJSON(ctx, http.MethodGet, "connections/"+url.PathEscape(targetConnectionID), query, nil)
	case "delete_connection":
		targetConnectionID, argErr := requireStringArg(args, "connection_id")
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.ControlJSON(ctx, http.MethodDelete, "connections/"+url.PathEscape(targetConnectionID), query, nil)
	default:
		return toolError(fmtUnsupportedGmailAction(action)), nil
	}
	if err != nil {
		return toolError(err), nil
	}

	return formatJSONOrText(payload), nil
}

func (m *MatonGmailTool) resolveClient() (*MatonClient, error) {
	if m.client != nil {
		return m.client, nil
	}
	return NewMatonClientFromEnv(m.httpClient)
}

func (m *MatonGmailTool) gmailPath(parts ...string) string {
	clean := make([]string, 0, len(parts)+5)
	clean = append(clean, matonGoogleMailApp, "gmail", "v1", "users", "me")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		clean = append(clean, strings.TrimPrefix(part, "/"))
	}
	return strings.Join(clean, "/")
}

func fmtUnsupportedGmailAction(action string) error {
	return errors.New("unsupported gmail action: " + action)
}

func normalizeSendMessageBody(message map[string]any) (map[string]any, error) {
	if len(message) == 0 {
		return nil, fmt.Errorf("message cannot be empty")
	}

	raw, hasRaw, err := optionalStringArg(message, "raw")
	if err != nil {
		return nil, err
	}
	if hasRaw {
		normalizedRaw, err := normalizeBase64URL(raw)
		if err != nil {
			return nil, fmt.Errorf("message.raw must be base64url-encoded RFC822 with no whitespace: %w", err)
		}
		payload := cloneObject(message)
		deleteStructuredMessageFields(payload)
		payload["raw"] = normalizedRaw
		return payload, nil
	}

	return buildRawMessageFromStructuredFields(message)
}

func buildRawMessageFromStructuredFields(message map[string]any) (map[string]any, error) {
	to, ok, err := optionalStringArg(message, "to")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("send_message requires message.raw or message.to with message.body")
	}

	subject, _, err := optionalStringArg(message, "subject")
	if err != nil {
		return nil, err
	}
	cc, _, err := optionalStringArg(message, "cc")
	if err != nil {
		return nil, err
	}
	bcc, _, err := optionalStringArg(message, "bcc")
	if err != nil {
		return nil, err
	}

	contentType, hasContentType, err := optionalStringArg(message, "content_type")
	if err != nil {
		return nil, err
	}

	body, hasBody, err := optionalStringArg(message, "body")
	if err != nil {
		return nil, err
	}
	if !hasBody {
		body, hasBody, err = optionalStringArg(message, "text")
		if err != nil {
			return nil, err
		}
	}
	usedHTML := false
	if !hasBody {
		body, hasBody, err = optionalStringArg(message, "html")
		if err != nil {
			return nil, err
		}
		usedHTML = hasBody
	}
	if !hasBody {
		return nil, fmt.Errorf("message.body (or message.text/message.html) is required when message.raw is not provided")
	}

	if !hasContentType {
		if usedHTML {
			contentType = "text/html; charset=utf-8"
		} else {
			contentType = "text/plain; charset=utf-8"
		}
	}

	rfc822 := buildRFC822Message(to, cc, bcc, subject, contentType, body)
	raw := base64.RawURLEncoding.EncodeToString([]byte(rfc822))

	payload := cloneObject(message)
	deleteStructuredMessageFields(payload)
	payload["raw"] = raw
	return payload, nil
}

func collectTopLevelMessageFields(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	keys := []string{"raw", "to", "subject", "body", "text", "html", "content_type", "cc", "bcc", "threadId", "labelIds"}
	message := map[string]any{}
	for _, key := range keys {
		if value, ok := args[key]; ok {
			message[key] = value
		}
	}
	if len(message) == 0 {
		return nil
	}
	return message
}

func deleteStructuredMessageFields(payload map[string]any) {
	delete(payload, "to")
	delete(payload, "subject")
	delete(payload, "cc")
	delete(payload, "bcc")
	delete(payload, "body")
	delete(payload, "text")
	delete(payload, "html")
	delete(payload, "content_type")
}

func normalizeBase64URL(raw string) (string, error) {
	clean := strings.Join(strings.Fields(raw), "")
	if clean == "" {
		return "", fmt.Errorf("value is empty")
	}

	decoded, err := decodeAnyBase64(clean)
	if err != nil {
		return "", err
	}
	if len(decoded) == 0 {
		return "", fmt.Errorf("decoded value is empty")
	}
	return base64.RawURLEncoding.EncodeToString(decoded), nil
}

func decodeAnyBase64(value string) ([]byte, error) {
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.URLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func buildRFC822Message(to, cc, bcc, subject, contentType, body string) string {
	headers := []string{
		"MIME-Version: 1.0",
		"To: " + to,
	}
	if cc != "" {
		headers = append(headers, "Cc: "+cc)
	}
	if bcc != "" {
		headers = append(headers, "Bcc: "+bcc)
	}
	if subject != "" {
		headers = append(headers, "Subject: "+encodeHeaderValue(subject))
	}
	headers = append(headers, "Content-Type: "+contentType)
	headers = append(headers, "Content-Transfer-Encoding: 8bit")

	return strings.Join(headers, "\r\n") + "\r\n\r\n" + normalizeCRLF(body)
}

func encodeHeaderValue(value string) string {
	for _, r := range value {
		if r > 127 {
			return mime.BEncoding.Encode("utf-8", value)
		}
	}
	return value
}

func normalizeCRLF(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func cloneObject(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value))
	for k, v := range value {
		clone[k] = v
	}
	return clone
}
