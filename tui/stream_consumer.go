package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/fernandopn/benoit/compression"
	"github.com/fernandopn/benoit/providers"
	tuicmd "github.com/fernandopn/benoit/tui/commands"
	tuiutils "github.com/fernandopn/benoit/tui/utils"
)

const compressionFinishedMessage = providers.DefaultCompressionFinishedMessage
const defaultCompressionMaxWords = tuicmd.DefaultCompressionMaxWords

type streamStarter func(context.Context, string) <-chan providers.Msg

type streamCallbacks struct {
	OnChat              func(string)
	OnReasoning         func(string)
	OnToolCall          func(name string, args string, callID string)
	OnToolResult        func(name string, args string, output string, callID string)
	OnContextUsage      func(value string, metadata map[string]string)
	OnCompressionStatus func(value string, metadata map[string]string)
	OnError             func(string)
}

func streamStartForProvider(provider providers.Provider, sessionID string) streamStarter {
	if provider == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	startChat := func(ctx context.Context, prompt string) <-chan providers.Msg {
		return provider.Chat(providers.WithSessionID(ctx, sessionID), prompt)
	}

	return func(ctx context.Context, prompt string) <-chan providers.Msg {
		if commandStream, ok := startCommandStream(ctx, provider, sessionID, prompt); ok {
			return commandStream
		}
		return startChat(ctx, prompt)
	}
}

func startCommandStream(ctx context.Context, provider providers.Provider, sessionID string, prompt string) (<-chan providers.Msg, bool) {
	maxWords, isCompress, parseErr := parseCompressCommand(prompt)
	if !isCompress {
		return nil, false
	}
	if parseErr != nil {
		return singleErrorStream(parseErr.Error()), true
	}

	out := make(chan providers.Msg, 4)
	go func() {
		defer close(out)
		compressor := compression.NewBasic(maxWords)
		summary, status, contextUsage, err := providers.PerformCompressionWithStatus(ctx, provider, sessionID, compressor, compressionFinishedMessage)
		if err != nil {
			out <- providers.Msg{Type: providers.MsgTypeError, Value: err.Error()}
			return
		}
		out <- status
		providers.NotifyCompressionStatusSent(provider, sessionID)
		if contextUsage.Type == providers.MsgTypeContextUsage {
			out <- contextUsage
		}
		out <- providers.Msg{Type: providers.MsgTypeChatDelta, Value: summary}
		out <- providers.Msg{Type: providers.MsgTypeChatFinal, Value: summary}
	}()
	return out, true
}

func parseCompressCommand(prompt string) (int, bool, error) {
	return tuicmd.ParseCompress(prompt)
}

func singleErrorStream(errText string) <-chan providers.Msg {
	out := make(chan providers.Msg, 1)
	out <- providers.Msg{Type: providers.MsgTypeError, Value: errText}
	close(out)
	return out
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
		case providers.MsgTypeCompressionStatus:
			if callbacks.OnCompressionStatus != nil {
				callbacks.OnCompressionStatus(msg.Value, msg.Metadata)
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
