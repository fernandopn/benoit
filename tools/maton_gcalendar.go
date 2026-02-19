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

const matonGoogleCalendarApp = "google-calendar"

// MatonGCalendarTool integrates Google Calendar through Maton managed OAuth.
type MatonGCalendarTool struct {
	client     *MatonClient
	httpClient httpDoer
}

func NewMatonGCalendarTool() *MatonGCalendarTool {
	return NewMatonGCalendarToolWithHTTPClient(http.DefaultClient)
}

func NewMatonGCalendarToolWithHTTPClient(httpClient httpDoer) *MatonGCalendarTool {
	return &MatonGCalendarTool{httpClient: httpClient}
}

func NewMatonGCalendarToolWithClient(client *MatonClient) *MatonGCalendarTool {
	return &MatonGCalendarTool{client: client}
}

func (m *MatonGCalendarTool) Name() string {
	return "maton_gcalendar"
}

func (m *MatonGCalendarTool) Definition() responses.ToolUnionParam {
	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name:        m.Name(),
			Description: openai.String("Google Calendar via Maton. Actions: list/get calendars, list/get/create/update/patch/delete events, quick add, free/busy, and connection management."),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type": "string",
						"enum": []string{
							"list_calendars",
							"get_calendar",
							"list_events",
							"get_event",
							"create_event",
							"update_event",
							"patch_event",
							"delete_event",
							"quick_add_event",
							"free_busy",
							"list_connections",
							"create_connection",
							"get_connection",
							"delete_connection",
						},
						"description": "Operation to perform",
					},
					"calendar_id": map[string]any{
						"type":        "string",
						"description": "Google Calendar ID (for example: primary)",
					},
					"event_id": map[string]any{
						"type":        "string",
						"description": "Google Calendar event ID",
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
					"event": map[string]any{
						"type":                 "object",
						"description":          "Event payload for create/update/patch actions",
						"additionalProperties": true,
					},
					"free_busy": map[string]any{
						"type":                 "object",
						"description":          "Body for freeBusy query",
						"additionalProperties": true,
					},
					"text": map[string]any{
						"type":        "string",
						"description": "Natural language text for quick add",
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

func (m *MatonGCalendarTool) Call(ctx context.Context, args map[string]any) (string, error) {
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
	case "list_calendars":
		payload, err = client.GatewayJSON(ctx, http.MethodGet, m.calendarPath("users", "me", "calendarList"), query, nil, connectionID)
	case "get_calendar":
		calendarID, argErr := requireStringArg(args, "calendar_id")
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodGet, m.calendarPath("calendars", url.PathEscape(calendarID)), query, nil, connectionID)
	case "list_events":
		calendarID, argErr := requireStringArg(args, "calendar_id")
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodGet, m.calendarPath("calendars", url.PathEscape(calendarID), "events"), query, nil, connectionID)
	case "get_event":
		calendarID, argErr := requireStringArg(args, "calendar_id")
		if argErr != nil {
			return toolError(argErr), nil
		}
		eventID, argErr := requireStringArg(args, "event_id")
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodGet, m.calendarPath("calendars", url.PathEscape(calendarID), "events", url.PathEscape(eventID)), query, nil, connectionID)
	case "create_event":
		calendarID, eventBody, argErr := m.requireCalendarAndEvent(args)
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodPost, m.calendarPath("calendars", url.PathEscape(calendarID), "events"), query, eventBody, connectionID)
	case "update_event":
		calendarID, eventID, eventBody, argErr := m.requireCalendarEventAndBody(args)
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodPut, m.calendarPath("calendars", url.PathEscape(calendarID), "events", url.PathEscape(eventID)), query, eventBody, connectionID)
	case "patch_event":
		calendarID, eventID, eventBody, argErr := m.requireCalendarEventAndBody(args)
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodPatch, m.calendarPath("calendars", url.PathEscape(calendarID), "events", url.PathEscape(eventID)), query, eventBody, connectionID)
	case "delete_event":
		calendarID, argErr := requireStringArg(args, "calendar_id")
		if argErr != nil {
			return toolError(argErr), nil
		}
		eventID, argErr := requireStringArg(args, "event_id")
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodDelete, m.calendarPath("calendars", url.PathEscape(calendarID), "events", url.PathEscape(eventID)), query, nil, connectionID)
	case "quick_add_event":
		calendarID, argErr := requireStringArg(args, "calendar_id")
		if argErr != nil {
			return toolError(argErr), nil
		}
		text, argErr := requireStringArg(args, "text")
		if argErr != nil {
			return toolError(argErr), nil
		}
		if query == nil {
			query = map[string]string{}
		}
		query["text"] = text
		payload, err = client.GatewayJSON(ctx, http.MethodPost, m.calendarPath("calendars", url.PathEscape(calendarID), "events", "quickAdd"), query, nil, connectionID)
	case "free_busy":
		freeBusyBody, argErr := requireObjectArg(args, "free_busy")
		if argErr != nil {
			return toolError(argErr), nil
		}
		payload, err = client.GatewayJSON(ctx, http.MethodPost, m.calendarPath("freeBusy"), query, freeBusyBody, connectionID)
	case "list_connections":
		if query == nil {
			query = map[string]string{}
		}
		if _, ok := query["app"]; !ok {
			query["app"] = matonGoogleCalendarApp
		}
		payload, err = client.ControlJSON(ctx, http.MethodGet, "connections", query, nil)
	case "create_connection":
		body := map[string]any{"app": matonGoogleCalendarApp}
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
		return toolError(fmtUnsupportedAction(action)), nil
	}
	if err != nil {
		return toolError(err), nil
	}

	return formatJSONOrText(payload), nil
}

func (m *MatonGCalendarTool) resolveClient() (*MatonClient, error) {
	if m.client != nil {
		return m.client, nil
	}
	return NewMatonClientFromEnv(m.httpClient)
}

func (m *MatonGCalendarTool) calendarPath(parts ...string) string {
	clean := make([]string, 0, len(parts)+3)
	clean = append(clean, matonGoogleCalendarApp, "calendar", "v3")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		clean = append(clean, strings.TrimPrefix(part, "/"))
	}
	return strings.Join(clean, "/")
}

func (m *MatonGCalendarTool) requireCalendarAndEvent(args map[string]any) (string, map[string]any, error) {
	calendarID, err := requireStringArg(args, "calendar_id")
	if err != nil {
		return "", nil, err
	}
	eventBody, err := requireObjectArg(args, "event")
	if err != nil {
		return "", nil, err
	}
	return calendarID, eventBody, nil
}

func (m *MatonGCalendarTool) requireCalendarEventAndBody(args map[string]any) (string, string, map[string]any, error) {
	calendarID, err := requireStringArg(args, "calendar_id")
	if err != nil {
		return "", "", nil, err
	}
	eventID, err := requireStringArg(args, "event_id")
	if err != nil {
		return "", "", nil, err
	}
	eventBody, err := requireObjectArg(args, "event")
	if err != nil {
		return "", "", nil, err
	}
	return calendarID, eventID, eventBody, nil
}

func fmtUnsupportedAction(action string) error {
	return errors.New("unsupported action: " + action)
}
