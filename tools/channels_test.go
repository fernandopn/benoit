package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fernandopn/benoit/channels"
)

type sendChannelStub struct {
	sent    []channels.ChannelMessage
	sendErr error
}

func (s *sendChannelStub) SendMessage(_ context.Context, message channels.ChannelMessage) error {
	s.sent = append(s.sent, message)
	return s.sendErr
}

func (s *sendChannelStub) RegisterReceiveMessageChan(_ chan<- channels.ChannelMessage) error {
	return nil
}

func (s *sendChannelStub) Listen(_ context.Context, _ int) error {
	return nil
}

func TestSendChannelMessageToolNameAndDefinition(t *testing.T) {
	tool := NewSendChannelMessageTool([]ChannelBinding{{Name: "telegram", Channel: &sendChannelStub{}}})
	if got := tool.Name(); got != "send_channel_message" {
		t.Fatalf("unexpected name: %q", got)
	}

	schema := tool.Schema()
	if schema.Kind != ToolKindFunction {
		t.Fatalf("expected function tool kind, got %q", schema.Kind)
	}
	if schema.Name != tool.Name() {
		t.Fatalf("unexpected schema name: %q", schema.Name)
	}
	params, err := schema.ParametersMap()
	if err != nil {
		t.Fatalf("parse parameters: %v", err)
	}
	properties, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %T", params["properties"])
	}
	if _, ok := properties["channel"]; !ok {
		t.Fatal("expected channel property")
	}
	if _, ok := properties["user_id"]; !ok {
		t.Fatal("expected user_id property")
	}
	if _, ok := properties["message"]; !ok {
		t.Fatal("expected message property")
	}
}

func TestSendChannelMessageToolValidation(t *testing.T) {
	tool := NewSendChannelMessageTool([]ChannelBinding{{Name: "telegram", Channel: &sendChannelStub{}}})

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "missing channel", args: map[string]any{"user_id": 1, "message": "hi"}, want: "missing required argument: channel"},
		{name: "unknown channel", args: map[string]any{"channel": "slack", "user_id": 1, "message": "hi"}, want: "unsupported channel"},
		{name: "missing user", args: map[string]any{"channel": "telegram", "message": "hi"}, want: "missing required argument: user_id"},
		{name: "invalid user type", args: map[string]any{"channel": "telegram", "user_id": true, "message": "hi"}, want: "user_id must be an integer"},
		{name: "zero user", args: map[string]any{"channel": "telegram", "user_id": 0, "message": "hi"}, want: "user_id must be greater than zero"},
		{name: "missing message", args: map[string]any{"channel": "telegram", "user_id": 1}, want: "missing required argument: message"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tool.Call(context.Background(), tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("unexpected output: %q (want contains %q)", out, tc.want)
			}
		})
	}
}

func TestSendChannelMessageToolNoChannelsConfigured(t *testing.T) {
	tool := NewSendChannelMessageTool(nil)
	out, err := tool.Call(context.Background(), map[string]any{"channel": "telegram", "user_id": 1, "message": "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: no channels configured" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestSendChannelMessageToolRequiresContext(t *testing.T) {
	tool := NewSendChannelMessageTool([]ChannelBinding{{Name: "telegram", Channel: &sendChannelStub{}}})
	out, err := tool.Call(nil, map[string]any{"channel": "telegram", "user_id": 1, "message": "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: context is required" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestSendChannelMessageToolSendSuccess(t *testing.T) {
	stub := &sendChannelStub{}
	tool := NewSendChannelMessageTool([]ChannelBinding{{Name: " TeLeGrAm ", Channel: stub}})

	out, err := tool.Call(context.Background(), map[string]any{
		"channel": " telegram ",
		"user_id": float64(42),
		"message": "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "sent message to telegram user 42" {
		t.Fatalf("unexpected output: %q", out)
	}
	if len(stub.sent) != 1 {
		t.Fatalf("expected one send call, got %d", len(stub.sent))
	}
	if stub.sent[0].Type != channels.TextMessage {
		t.Fatalf("unexpected message type: %d", stub.sent[0].Type)
	}
	if stub.sent[0].UserID != 42 || stub.sent[0].Text != "hello" {
		t.Fatalf("unexpected sent message: %#v", stub.sent[0])
	}
}

func TestSendChannelMessageToolSendError(t *testing.T) {
	stub := &sendChannelStub{sendErr: errors.New("send failed")}
	tool := NewSendChannelMessageTool([]ChannelBinding{{Name: "telegram", Channel: stub}})

	out, err := tool.Call(context.Background(), map[string]any{
		"channel": "telegram",
		"user_id": int64(7),
		"message": "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "error: send failed" {
		t.Fatalf("unexpected output: %q", out)
	}
}
