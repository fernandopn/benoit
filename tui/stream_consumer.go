package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/fernandopn/benoit/providers"
	tuiutils "github.com/fernandopn/benoit/tui/utils"
)

type streamStarter func(context.Context, string) <-chan providers.Msg

type streamCallbacks struct {
	OnChat         func(string)
	OnReasoning    func(string)
	OnToolCall     func(name string, args string, callID string)
	OnToolResult   func(name string, args string, output string, callID string)
	OnContextUsage func(value string, metadata map[string]string)
	OnError        func(string)
}

func streamStartForProvider(provider providers.Provider, sessionID string) streamStarter {
	if provider == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" {
		if sessionProvider, ok := provider.(providers.SessionProvider); ok {
			return func(ctx context.Context, prompt string) <-chan providers.Msg {
				return sessionProvider.ChatInSession(ctx, prompt, sessionID)
			}
		}
	}
	return provider.Chat
}

func streamPrompt(ctx context.Context, prompt string, timeout time.Duration, start streamStarter, callbacks streamCallbacks) (string, error) {
	if start == nil {
		return "", errors.New("provider stream is not configured")
	}

	requestCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	msgs := start(requestCtx, prompt)
	var (
		chatDelta         strings.Builder
		chatFinal         strings.Builder
		reasoningHasDelta bool
		streamErr         error
		pending           = map[string]pendingToolCall{}
	)

	for msg := range msgs {
		switch msg.Type {
		case providers.MsgTypeChatDelta:
			chatDelta.WriteString(msg.Value)
			if callbacks.OnChat != nil {
				callbacks.OnChat(msg.Value)
			}
		case providers.MsgTypeChatFinal:
			chatFinal.WriteString(msg.Value)
		case providers.MsgTypeReasoningSummaryDelta:
			reasoningHasDelta = true
			if callbacks.OnReasoning != nil {
				callbacks.OnReasoning(msg.Value)
			}
		case providers.MsgTypeReasoningSummaryFinal:
			if !reasoningHasDelta && callbacks.OnReasoning != nil {
				callbacks.OnReasoning(msg.Value)
			}
		case providers.MsgTypeToolCall:
			callID := strings.TrimSpace(msg.Metadata["call_id"])
			name := strings.TrimSpace(msg.Metadata["tool"])
			args := tuiutils.CompactWhitespace(strings.TrimSpace(msg.Value))
			if args == "" {
				args = "{}"
			}
			if callID != "" {
				pending[callID] = pendingToolCall{name: name, args: args}
			}
			if callbacks.OnToolCall != nil {
				callbacks.OnToolCall(name, args, callID)
			}
		case providers.MsgTypeToolResult:
			callID := strings.TrimSpace(msg.Metadata["call_id"])
			name := strings.TrimSpace(msg.Metadata["tool"])
			args := "{}"
			if callID != "" {
				if call, ok := pending[callID]; ok {
					if name == "" {
						name = call.name
					}
					args = call.args
					delete(pending, callID)
				}
			}
			if args == "{}" {
				if rawArgs := strings.TrimSpace(msg.Metadata["args"]); rawArgs != "" {
					args = tuiutils.CompactWhitespace(rawArgs)
				}
			}
			output := strings.TrimSpace(msg.Value)
			if output == "" {
				output = "(empty output)"
			}
			if callbacks.OnToolResult != nil {
				callbacks.OnToolResult(name, args, output, callID)
			}
		case providers.MsgTypeContextUsage:
			if callbacks.OnContextUsage != nil {
				callbacks.OnContextUsage(msg.Value, msg.Metadata)
			}
		case providers.MsgTypeError:
			errText := strings.TrimSpace(msg.Value)
			if errText == "" {
				errText = "provider returned an empty error"
			}
			if streamErr == nil {
				streamErr = errors.New(errText)
			}
			if callbacks.OnError != nil {
				callbacks.OnError(errText)
			}
		}
	}

	reply := chatFinal.String()
	if strings.TrimSpace(reply) == "" {
		reply = chatDelta.String()
	}

	if streamErr != nil {
		return reply, streamErr
	}
	if err := requestCtx.Err(); err != nil {
		return reply, err
	}
	if strings.TrimSpace(reply) == "" {
		return "(empty response)", nil
	}
	return reply, nil
}
