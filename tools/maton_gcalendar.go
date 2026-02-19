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
	client *MatonClient
}

type gcalendarActionHandler func(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error)

func NewMatonGCalendarTool(client *MatonClient) *MatonGCalendarTool {
	return &MatonGCalendarTool{client: client}
}

func NewMatonGCalendarToolWithClient(client *MatonClient) *MatonGCalendarTool {
	return NewMatonGCalendarTool(client)
}

func (m *MatonGCalendarTool) Name() string {
	return "maton_gcalendar"
}

func (m *MatonGCalendarTool) Definition() responses.ToolUnionParam {
	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name:        m.Name(),
			Description: openai.String("Google Calendar via Maton. Actions: list/get calendars, list/get/create/update/patch/delete events, quick add, free/busy, and connection management. For list_events, always pass query.timeMin and query.timeMax (RFC3339) to bound results."),
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
						"type":        "object",
						"description": "Query parameters for the selected action. For list_events, include timeMin and timeMax (RFC3339).",
						"properties": map[string]any{
							"timeMin": map[string]any{
								"type":        "string",
								"description": "Start of time range for list_events in RFC3339 (inclusive)",
							},
							"timeMax": map[string]any{
								"type":        "string",
								"description": "End of time range for list_events in RFC3339 (exclusive)",
							},
						},
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

	handler, ok := m.actionHandlers()[action]
	if !ok {
		return toolError(fmtUnsupportedAction(action)), nil
	}
	payload, err := handler(ctx, client, args, query, connectionID)
	if err != nil {
		return toolError(err), nil
	}

	return formatJSONOrText(payload), nil
}

func (m *MatonGCalendarTool) resolveClient() (*MatonClient, error) {
	if m == nil || m.client == nil {
		return nil, errors.New("maton client is not configured")
	}
	return m.client, nil
}

func (m *MatonGCalendarTool) actionHandlers() map[string]gcalendarActionHandler {
	return map[string]gcalendarActionHandler{
		"list_calendars":    m.handleListCalendars,
		"get_calendar":      m.handleGetCalendar,
		"list_events":       m.handleListEvents,
		"get_event":         m.handleGetEvent,
		"create_event":      m.handleCreateEvent,
		"update_event":      m.handleUpdateEvent,
		"patch_event":       m.handlePatchEvent,
		"delete_event":      m.handleDeleteEvent,
		"quick_add_event":   m.handleQuickAddEvent,
		"free_busy":         m.handleFreeBusy,
		"list_connections":  m.handleListConnections,
		"create_connection": m.handleCreateConnection,
		"get_connection":    m.handleGetConnection,
		"delete_connection": m.handleDeleteConnection,
	}
}

func (m *MatonGCalendarTool) handleListCalendars(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error) {
	_ = args
	return client.GatewayJSON(ctx, http.MethodGet, m.calendarPath("users", "me", "calendarList"), query, nil, connectionID)
}

func (m *MatonGCalendarTool) handleGetCalendar(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error) {
	calendarID, err := requireStringArg(args, "calendar_id")
	if err != nil {
		return nil, err
	}
	return client.GatewayJSON(ctx, http.MethodGet, m.calendarPath("calendars", url.PathEscape(calendarID)), query, nil, connectionID)
}

func (m *MatonGCalendarTool) handleListEvents(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error) {
	calendarID, err := requireStringArg(args, "calendar_id")
	if err != nil {
		return nil, err
	}
	if err := requireListEventsTimeRange(query); err != nil {
		return nil, err
	}
	return client.GatewayJSON(ctx, http.MethodGet, m.calendarPath("calendars", url.PathEscape(calendarID), "events"), query, nil, connectionID)
}

func (m *MatonGCalendarTool) handleGetEvent(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error) {
	calendarID, err := requireStringArg(args, "calendar_id")
	if err != nil {
		return nil, err
	}
	eventID, err := requireStringArg(args, "event_id")
	if err != nil {
		return nil, err
	}
	return client.GatewayJSON(ctx, http.MethodGet, m.calendarPath("calendars", url.PathEscape(calendarID), "events", url.PathEscape(eventID)), query, nil, connectionID)
}

func (m *MatonGCalendarTool) handleCreateEvent(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error) {
	calendarID, eventBody, err := m.requireCalendarAndEvent(args)
	if err != nil {
		return nil, err
	}
	return client.GatewayJSON(ctx, http.MethodPost, m.calendarPath("calendars", url.PathEscape(calendarID), "events"), query, eventBody, connectionID)
}

func (m *MatonGCalendarTool) handleUpdateEvent(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error) {
	calendarID, eventID, eventBody, err := m.requireCalendarEventAndBody(args)
	if err != nil {
		return nil, err
	}
	return client.GatewayJSON(ctx, http.MethodPut, m.calendarPath("calendars", url.PathEscape(calendarID), "events", url.PathEscape(eventID)), query, eventBody, connectionID)
}

func (m *MatonGCalendarTool) handlePatchEvent(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error) {
	calendarID, eventID, eventBody, err := m.requireCalendarEventAndBody(args)
	if err != nil {
		return nil, err
	}
	return client.GatewayJSON(ctx, http.MethodPatch, m.calendarPath("calendars", url.PathEscape(calendarID), "events", url.PathEscape(eventID)), query, eventBody, connectionID)
}

func (m *MatonGCalendarTool) handleDeleteEvent(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error) {
	calendarID, err := requireStringArg(args, "calendar_id")
	if err != nil {
		return nil, err
	}
	eventID, err := requireStringArg(args, "event_id")
	if err != nil {
		return nil, err
	}
	return client.GatewayJSON(ctx, http.MethodDelete, m.calendarPath("calendars", url.PathEscape(calendarID), "events", url.PathEscape(eventID)), query, nil, connectionID)
}

func (m *MatonGCalendarTool) handleQuickAddEvent(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error) {
	calendarID, err := requireStringArg(args, "calendar_id")
	if err != nil {
		return nil, err
	}
	text, err := requireStringArg(args, "text")
	if err != nil {
		return nil, err
	}
	if query == nil {
		query = map[string]string{}
	}
	query["text"] = text
	return client.GatewayJSON(ctx, http.MethodPost, m.calendarPath("calendars", url.PathEscape(calendarID), "events", "quickAdd"), query, nil, connectionID)
}

func (m *MatonGCalendarTool) handleFreeBusy(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error) {
	freeBusyBody, err := requireObjectArg(args, "free_busy")
	if err != nil {
		return nil, err
	}
	return client.GatewayJSON(ctx, http.MethodPost, m.calendarPath("freeBusy"), query, freeBusyBody, connectionID)
}

func (m *MatonGCalendarTool) handleListConnections(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error) {
	_ = args
	_ = connectionID
	if query == nil {
		query = map[string]string{}
	}
	if _, ok := query["app"]; !ok {
		query["app"] = matonGoogleCalendarApp
	}
	return client.ControlJSON(ctx, http.MethodGet, "connections", query, nil)
}

func (m *MatonGCalendarTool) handleCreateConnection(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error) {
	_ = connectionID
	body := map[string]any{"app": matonGoogleCalendarApp}
	if metadata, ok, err := optionalObjectArg(args, "metadata"); err != nil {
		return nil, err
	} else if ok {
		body["metadata"] = metadata
	}
	return client.ControlJSON(ctx, http.MethodPost, "connections", query, body)
}

func (m *MatonGCalendarTool) handleGetConnection(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error) {
	_ = connectionID
	targetConnectionID, err := requireStringArg(args, "connection_id")
	if err != nil {
		return nil, err
	}
	return client.ControlJSON(ctx, http.MethodGet, "connections/"+url.PathEscape(targetConnectionID), query, nil)
}

func (m *MatonGCalendarTool) handleDeleteConnection(ctx context.Context, client *MatonClient, args map[string]any, query map[string]string, connectionID string) ([]byte, error) {
	_ = connectionID
	targetConnectionID, err := requireStringArg(args, "connection_id")
	if err != nil {
		return nil, err
	}
	return client.ControlJSON(ctx, http.MethodDelete, "connections/"+url.PathEscape(targetConnectionID), query, nil)
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

func requireListEventsTimeRange(query map[string]string) error {
	timeMin := strings.TrimSpace(query["timeMin"])
	timeMax := strings.TrimSpace(query["timeMax"])
	if timeMin == "" || timeMax == "" {
		return errors.New("list_events requires query.timeMin and query.timeMax (RFC3339)")
	}
	return nil
}
