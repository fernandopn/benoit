package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/fernandopn/benoit/channels"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// ChannelBinding associates a tool-facing channel name with an implementation.
type ChannelBinding struct {
	Name    string
	Channel channels.Channel
}

// SendChannelMessageTool sends text messages through configured channels.
type SendChannelMessageTool struct {
	channels map[string]channels.Channel
}

func NewSendChannelMessageTool(bindings []ChannelBinding) *SendChannelMessageTool {
	channelMap := make(map[string]channels.Channel, len(bindings))
	for _, binding := range bindings {
		name := normalizeChannelName(binding.Name)
		if name == "" || binding.Channel == nil {
			continue
		}
		channelMap[name] = binding.Channel
	}
	return &SendChannelMessageTool{channels: channelMap}
}

func (s *SendChannelMessageTool) Name() string {
	return "send_channel_message"
}

func (s *SendChannelMessageTool) Definition() responses.ToolUnionParam {
	description := "Send a text message to a configured channel user."
	if names := s.channelNames(); len(names) > 0 {
		description += " Configured channels: " + strings.Join(names, ", ") + "."
	}
	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name:        s.Name(),
			Description: openai.String(description),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel": map[string]any{
						"type":        "string",
						"description": "Channel name (for example: telegram)",
					},
					"user_id": map[string]any{
						"type":        "integer",
						"description": "Recipient identifier inside the selected channel",
					},
					"message": map[string]any{
						"type":        "string",
						"description": "Text message to send",
					},
				},
				"required":             []string{"channel", "user_id", "message"},
				"additionalProperties": false,
			},
			Strict: openai.Bool(true),
		},
	}
}

func (s *SendChannelMessageTool) Call(ctx context.Context, args map[string]any) (string, error) {
	if ctx == nil {
		return toolError(fmt.Errorf("context is required")), nil
	}
	if s == nil || len(s.channels) == 0 {
		return "error: no channels configured", nil
	}

	channelName, err := requireStringArg(args, "channel")
	if err != nil {
		return toolError(err), nil
	}
	channelName = normalizeChannelName(channelName)
	channel, ok := s.channels[channelName]
	if !ok {
		return toolError(fmt.Errorf("unsupported channel: %s", channelName)), nil
	}

	userID, err := requireInt64Arg(args, "user_id")
	if err != nil {
		return toolError(err), nil
	}
	if userID <= 0 {
		return toolError(fmt.Errorf("user_id must be greater than zero")), nil
	}

	message, err := requireStringArg(args, "message")
	if err != nil {
		return toolError(err), nil
	}

	if err := channel.SendMessage(ctx, channels.ChannelMessage{UserID: userID, Type: channels.TextMessage, Text: message}); err != nil {
		return toolError(err), nil
	}

	return fmt.Sprintf("sent message to %s user %d", channelName, userID), nil
}

func (s *SendChannelMessageTool) channelNames() []string {
	if s == nil || len(s.channels) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.channels))
	for name := range s.channels {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func normalizeChannelName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func requireInt64Arg(args map[string]any, key string) (int64, error) {
	if args == nil {
		return 0, fmt.Errorf("missing required argument: %s", key)
	}
	raw, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("missing required argument: %s", key)
	}

	switch value := raw.(type) {
	case int:
		return int64(value), nil
	case int8:
		return int64(value), nil
	case int16:
		return int64(value), nil
	case int32:
		return int64(value), nil
	case int64:
		return value, nil
	case uint:
		if uint64(value) > math.MaxInt64 {
			return 0, fmt.Errorf("%s is out of range", key)
		}
		return int64(value), nil
	case uint8:
		return int64(value), nil
	case uint16:
		return int64(value), nil
	case uint32:
		return int64(value), nil
	case uint64:
		if value > math.MaxInt64 {
			return 0, fmt.Errorf("%s is out of range", key)
		}
		return int64(value), nil
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value {
			return 0, fmt.Errorf("%s must be an integer", key)
		}
		if value < math.MinInt64 || value > math.MaxInt64 {
			return 0, fmt.Errorf("%s is out of range", key)
		}
		return int64(value), nil
	case float32:
		floatValue := float64(value)
		if math.IsNaN(floatValue) || math.IsInf(floatValue, 0) || math.Trunc(floatValue) != floatValue {
			return 0, fmt.Errorf("%s must be an integer", key)
		}
		if floatValue < math.MinInt64 || floatValue > math.MaxInt64 {
			return 0, fmt.Errorf("%s is out of range", key)
		}
		return int64(floatValue), nil
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer", key)
		}
		return parsed, nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, fmt.Errorf("%s cannot be empty", key)
		}
		parsed, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer", key)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("%s must be an integer", key)
	}
}
