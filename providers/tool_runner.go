package providers

import (
	"context"
	"fmt"
	"sync"

	"github.com/fernandopn/benoid/tools"
	"github.com/openai/openai-go/v3/responses"
)

type functionCall struct {
	name    string
	callID  string
	rawArgs string
}

type toolCall struct {
	name   string
	callID string
	args   map[string]any
	raw    string
	tool   tools.Tool
}

type toolResult struct {
	output string
	err    error
}

type toolRunner interface {
	Run(ctx context.Context, calls []toolCall) []toolResult
}

type parallelToolRunner struct{}

func (parallelToolRunner) Run(ctx context.Context, calls []toolCall) []toolResult {
	results := make([]toolResult, len(calls))
	var wg sync.WaitGroup
	wg.Add(len(calls))
	for i, call := range calls {
		go func(idx int, call toolCall) {
			defer wg.Done()
			output, err := call.tool.Call(ctx, call.args)
			results[idx] = toolResult{output: output, err: err}
		}(i, call)
	}
	wg.Wait()
	return results
}

func functionCallsFromResponse(resp *responses.Response) []functionCall {
	if resp == nil || len(resp.Output) == 0 {
		return nil
	}
	calls := make([]functionCall, 0)
	for _, item := range resp.Output {
		if item.Type != "function_call" {
			continue
		}
		call := item.AsFunctionCall()
		if call.Name == "" {
			continue
		}
		calls = append(calls, functionCall{name: call.Name, callID: call.CallID, rawArgs: call.Arguments})
	}
	return calls
}

func buildToolCalls(calls []functionCall, toolMap map[string]tools.Tool) ([]toolCall, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	if toolMap == nil {
		return nil, fmt.Errorf("tool call received but no tools are configured")
	}
	toolCalls := make([]toolCall, 0, len(calls))
	for _, call := range calls {
		tool, ok := toolMap[call.name]
		if !ok {
			return nil, fmt.Errorf("tool not found: %s", call.name)
		}
		if call.callID == "" {
			return nil, fmt.Errorf("tool call missing call_id: %s", call.name)
		}
		args, err := parseToolArgs(call.rawArgs)
		if err != nil {
			return nil, fmt.Errorf("invalid arguments for %s: %w", call.name, err)
		}
		toolCalls = append(toolCalls, toolCall{
			name:   call.name,
			callID: call.callID,
			args:   args,
			raw:    call.rawArgs,
			tool:   tool,
		})
	}
	return toolCalls, nil
}
