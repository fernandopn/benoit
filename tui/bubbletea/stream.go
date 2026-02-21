package bubbletea

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fernandopn/benoit/providers"
)

func startStream(ctx context.Context, starter StreamStarter, prompt string, seq int) tea.Cmd {
	return func() tea.Msg {
		ch, cancel, err := starter(ctx, prompt)
		if cancel == nil {
			cancel = func() {}
		}
		if err != nil {
			return streamStartFailedMsg{Seq: seq, Err: err, Cancel: cancel}
		}
		if ch == nil {
			cancel()
			return streamStartFailedMsg{Seq: seq, Err: errors.New("start stream callback returned nil channel")}
		}
		return streamStartedMsg{Seq: seq, Ch: ch, Cancel: cancel}
	}
}

func readStreamChunk(ch <-chan providers.Msg, maxWait time.Duration, maxMsgs int, seq int) tea.Cmd {
	return func() tea.Msg {
		var items []providers.Msg

		first, ok := <-ch
		if !ok {
			return streamChunkMsg{Seq: seq, Done: true}
		}
		items = append(items, first)

		timer := time.NewTimer(maxWait)
		defer timer.Stop()

		for len(items) < maxMsgs {
			select {
			case msg, ok := <-ch:
				if !ok {
					return streamChunkMsg{Seq: seq, Msgs: items, Done: true}
				}
				items = append(items, msg)
			case <-timer.C:
				return streamChunkMsg{Seq: seq, Msgs: items, Done: false}
			}
		}

		return streamChunkMsg{Seq: seq, Msgs: items, Done: false}
	}
}

func (m *model) applyStreamMessages(msgs []providers.Msg) {
	for _, msg := range msgs {
		switch msg.Type {
		case providers.MsgTypeChatDelta:
			m.appendToBlock(blockAssistant, msg.Value, nil)
		case providers.MsgTypeChatFinal:
			// Final messages are emitted for consumers that need complete text.
			// Bubble Tea already renders deltas incrementally.
		case providers.MsgTypeReasoningSummaryDelta:
			m.appendToBlock(blockReasoning, msg.Value, nil)
		case providers.MsgTypeReasoningSummaryFinal:
			// Final messages are emitted for consumers that need complete text.
			// Bubble Tea already renders deltas incrementally.
		case providers.MsgTypeToolCall:
			m.appendToolCall(msg.Value, msg.Metadata)
		case providers.MsgTypeToolResult:
			m.appendToolResult(msg.Value, msg.Metadata)
		case providers.MsgTypeContextUsage:
			m.updateContextUsage(msg.Value, msg.Metadata)
		case providers.MsgTypeCompressionStatus:
			status := strings.TrimSpace(msg.Value)
			if status != "" {
				m.appendBlock(blockSystem, status, msg.Metadata)
			}
		case providers.MsgTypeError:
			errText := strings.TrimSpace(msg.Value)
			if errText == "" {
				errText = "provider error"
			}
			m.markPendingToolsAsError(errText)
			m.appendBlock(blockError, errText, msg.Metadata)
			m.streaming = false
			m.cancelStreamIfAny()
		}
	}
}

func (m *model) appendToBlock(kind blockKind, text string, meta map[string]string) {
	if kind == blockContext || kind == blockError {
		m.appendBlock(kind, text, meta)
		return
	}

	if len(m.blocks) > 0 {
		last := &m.blocks[len(m.blocks)-1]
		if last.Kind == kind && compatibleMeta(kind, last.Meta, meta) {
			last.Text += text
			return
		}
	}
	m.appendBlock(kind, text, meta)
}

func (m *model) appendToolCall(text string, meta map[string]string) {
	callID := ""
	toolName := ""
	if meta != nil {
		callID = meta["call_id"]
		toolName = meta["tool"]
	}

	if callID != "" {
		if idx, ok := m.toolBlockIndex[callID]; ok && idx >= 0 && idx < len(m.blocks) {
			block := &m.blocks[idx]
			if block.Kind == blockToolWidget {
				block.ToolArgs += text
				ensureToolMeta(block, callID, toolName)
				if block.ToolState != toolExecutionError && strings.TrimSpace(block.ToolResult) == "" {
					block.ToolState = toolExecutionPending
				}
				return
			}
		}
	}

	newBlock := block{
		Kind:      blockToolWidget,
		Meta:      cloneMeta(meta),
		ToolArgs:  text,
		ToolState: toolExecutionPending,
	}
	if callID != "" {
		ensureToolMeta(&newBlock, callID, toolName)
		m.toolBlockIndex[callID] = len(m.blocks)
	}
	m.blocks = append(m.blocks, newBlock)
}

func (m *model) appendToolResult(text string, meta map[string]string) {
	callID := ""
	toolName := ""
	if meta != nil {
		callID = meta["call_id"]
		toolName = meta["tool"]
	}

	if callID != "" {
		if idx, ok := m.toolBlockIndex[callID]; ok && idx >= 0 && idx < len(m.blocks) {
			block := &m.blocks[idx]
			if block.Kind == blockToolWidget {
				block.ToolResult += text
				ensureToolMeta(block, callID, toolName)
				block.ToolState = toolExecutionDone
				return
			}
		}
	}

	newBlock := block{
		Kind:       blockToolWidget,
		Meta:       cloneMeta(meta),
		ToolResult: text,
		ToolState:  toolExecutionDone,
	}
	if callID != "" {
		ensureToolMeta(&newBlock, callID, toolName)
		m.toolBlockIndex[callID] = len(m.blocks)
	}
	m.blocks = append(m.blocks, newBlock)
}

func ensureToolMeta(b *block, callID, toolName string) {
	if b.Meta == nil {
		b.Meta = make(map[string]string, 2)
	}
	if callID != "" && b.Meta["call_id"] == "" {
		b.Meta["call_id"] = callID
	}
	if toolName != "" && b.Meta["tool"] == "" {
		b.Meta["tool"] = toolName
	}
}

func (m *model) appendBlock(kind blockKind, text string, meta map[string]string) {
	m.blocks = append(m.blocks, block{
		Kind: kind,
		Text: text,
		Meta: cloneMeta(meta),
	})
}

func (m *model) markPendingToolsAsError(errText string) {
	errText = strings.TrimSpace(errText)
	for i := range m.blocks {
		if m.blocks[i].Kind != blockToolWidget {
			continue
		}
		if m.blocks[i].ToolState != toolExecutionPending {
			continue
		}
		m.blocks[i].ToolState = toolExecutionError
		if strings.TrimSpace(m.blocks[i].ToolResult) == "" && errText != "" {
			m.blocks[i].ToolResult = errText
		}
	}
}

func compatibleMeta(kind blockKind, current, incoming map[string]string) bool {
	if kind != blockToolCall && kind != blockToolResult {
		return true
	}
	if current == nil || incoming == nil {
		return false
	}
	if current["call_id"] == "" || incoming["call_id"] == "" {
		return false
	}
	return current["call_id"] == incoming["call_id"]
}

func cloneMeta(meta map[string]string) map[string]string {
	if len(meta) == 0 {
		return nil
	}
	clone := make(map[string]string, len(meta))
	for k, v := range meta {
		clone[k] = v
	}
	return clone
}

func (m *model) hasPendingToolResults() bool {
	for i := range m.blocks {
		if m.blocks[i].Kind != blockToolWidget {
			continue
		}
		if m.blocks[i].ToolState == toolExecutionPending && strings.TrimSpace(m.blocks[i].ToolResult) == "" {
			return true
		}
	}
	return false
}

func (m *model) syncToolSpinner() tea.Cmd {
	if m.hasPendingToolResults() {
		if !m.toolSpinnerActive {
			m.toolSpinnerActive = true
			m.toolSpinnerIndex = 0
			return m.nextToolSpinnerTick()
		}
		return nil
	}
	m.toolSpinnerActive = false
	return nil
}

func (m model) nextToolSpinnerTick() tea.Cmd {
	return tea.Tick(140*time.Millisecond, func(time.Time) tea.Msg {
		return toolSpinnerTick{}
	})
}

func (m *model) cancelStreamIfAny() {
	if m.streamCancel != nil {
		m.streamCancel()
	}
	m.streamCancel = nil
	m.streamCh = nil
}
