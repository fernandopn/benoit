package tools

import (
	"context"
	"errors"
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
			Description: openai.String("Gmail via Maton. Actions: list/get messages, send/modify/trash messages, list/get threads, list labels, create/send drafts, get profile, and connection management."),
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
						"description":          "Message payload (for send_message)",
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
		messageBody, argErr := requireObjectArg(args, "message")
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodPost, m.gmailPath("messages", "send"), query, messageBody, connectionID)
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
