package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	matonAuthHeader       = "Authorization"
	matonConnectionHeader = "Maton-Connection"

	matonGatewayBaseURL = "https://gateway.maton.ai"
	matonControlBaseURL = "https://ctrl.maton.ai"
)

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// MatonClient wraps authenticated requests to Maton gateway/control APIs.
type MatonClient struct {
	apiKey         string
	gatewayBaseURL string
	controlBaseURL string
	httpClient     httpDoer
}

func NewMatonClient(apiKey string, httpClient httpDoer) (*MatonClient, error) {
	return NewMatonClientWithBaseURLs(apiKey, matonGatewayBaseURL, matonControlBaseURL, httpClient)
}

func NewMatonClientWithBaseURLs(apiKey, gatewayBaseURL, controlBaseURL string, httpClient httpDoer) (*MatonClient, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("api key cannot be empty")
	}
	gatewayBaseURL = strings.TrimSpace(gatewayBaseURL)
	if gatewayBaseURL == "" {
		return nil, fmt.Errorf("gateway base url cannot be empty")
	}
	controlBaseURL = strings.TrimSpace(controlBaseURL)
	if controlBaseURL == "" {
		return nil, fmt.Errorf("control base url cannot be empty")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &MatonClient{
		apiKey:         apiKey,
		gatewayBaseURL: gatewayBaseURL,
		controlBaseURL: controlBaseURL,
		httpClient:     httpClient,
	}, nil
}

func (c *MatonClient) GatewayJSON(ctx context.Context, method, path string, query map[string]string, body any, connectionID string) ([]byte, error) {
	return c.requestJSON(ctx, c.gatewayBaseURL, method, path, query, body, connectionID)
}

func (c *MatonClient) ControlJSON(ctx context.Context, method, path string, query map[string]string, body any) ([]byte, error) {
	return c.requestJSON(ctx, c.controlBaseURL, method, path, query, body, "")
}

func (c *MatonClient) requestJSON(ctx context.Context, baseURL, method, path string, query map[string]string, body any, connectionID string) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("maton client is not configured")
	}
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if c.httpClient == nil {
		return nil, fmt.Errorf("http client is not configured")
	}

	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return nil, fmt.Errorf("http method is required")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	requestURL, err := buildMatonURL(baseURL, path, query)
	if err != nil {
		return nil, err
	}

	var requestBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		requestBody = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, requestBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set(matonAuthHeader, "Bearer "+c.apiKey)
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	connectionID = strings.TrimSpace(connectionID)
	if connectionID != "" {
		req.Header.Set(matonConnectionHeader, connectionID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		errorBody := strings.TrimSpace(string(payload))
		if errorBody == "" {
			return nil, fmt.Errorf("maton request failed: %s", resp.Status)
		}
		return nil, fmt.Errorf("maton request failed: %s: %s", resp.Status, errorBody)
	}

	return payload, nil
}

func buildMatonURL(baseURL, path string, query map[string]string) (string, error) {
	cleanBase := strings.TrimSpace(baseURL)
	if cleanBase == "" {
		return "", fmt.Errorf("base url cannot be empty")
	}
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	resolvedURL, err := url.JoinPath(cleanBase, strings.TrimPrefix(cleanPath, "/"))
	if err != nil {
		return "", err
	}

	parsed, err := url.Parse(resolvedURL)
	if err != nil {
		return "", err
	}
	if len(query) > 0 {
		values := parsed.Query()
		for key, value := range query {
			if strings.TrimSpace(key) == "" {
				continue
			}
			values.Set(key, value)
		}
		parsed.RawQuery = values.Encode()
	}

	return parsed.String(), nil
}

func toolError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("error: %v", err)
}

func requireStringArg(args map[string]any, key string) (string, error) {
	if args == nil {
		return "", fmt.Errorf("missing required argument: %s", key)
	}
	raw, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument: %s", key)
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s cannot be empty", key)
	}
	return value, nil
}

func optionalStringArg(args map[string]any, key string) (string, bool, error) {
	if args == nil {
		return "", false, nil
	}
	raw, ok := args[key]
	if !ok {
		return "", false, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", false, fmt.Errorf("%s must be a string", key)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false, nil
	}
	return value, true, nil
}

func requireObjectArg(args map[string]any, key string) (map[string]any, error) {
	if args == nil {
		return nil, fmt.Errorf("missing required argument: %s", key)
	}
	raw, ok := args[key]
	if !ok {
		return nil, fmt.Errorf("missing required argument: %s", key)
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	return object, nil
}

func optionalObjectArg(args map[string]any, key string) (map[string]any, bool, error) {
	if args == nil {
		return nil, false, nil
	}
	raw, ok := args[key]
	if !ok {
		return nil, false, nil
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("%s must be an object", key)
	}
	return object, true, nil
}

func objectToQuery(value map[string]any) (map[string]string, error) {
	if len(value) == 0 {
		return nil, nil
	}
	query := make(map[string]string, len(value))
	for key, raw := range value {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		encoded, err := stringifyQueryValue(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid query value for %s: %w", key, err)
		}
		query[key] = encoded
	}
	if len(query) == 0 {
		return nil, nil
	}
	return query, nil
}

func stringifyQueryValue(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case json.Number:
		return v.String(), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), nil
	case int:
		return strconv.FormatInt(int64(v), 10), nil
	case int8:
		return strconv.FormatInt(int64(v), 10), nil
	case int16:
		return strconv.FormatInt(int64(v), 10), nil
	case int32:
		return strconv.FormatInt(int64(v), 10), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	default:
		payload, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(payload), nil
	}
}

func formatJSONOrText(payload []byte) string {
	if len(payload) == 0 {
		return "ok"
	}
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return "ok"
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return trimmed
	}
	formatted, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return trimmed
	}
	return string(formatted)
}
