package bubbletea

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/fernandopn/benoit/providers"
)

func newTestModel() model {
	return newModel(context.Background(), Config{
		ProviderName: "simple-provider",
		WelcomeText:  DefaultWelcomeText,
		HelpText:     DefaultHelpText,
		StartStream: func(context.Context, string) (<-chan providers.Msg, context.CancelFunc, error) {
			out := make(chan providers.Msg)
			close(out)
			return out, func() {}, nil
		},
	})
}

func TestRenderToolWidgetCollapsesResult(t *testing.T) {
	m := newTestModel()
	m.vp.Width = 120

	rendered, expandable := m.renderToolWidget(block{
		Kind:       blockToolWidget,
		ToolArgs:   `{"query":"test"}`,
		ToolResult: "line-1\nline-2\nline-3\nline-4\nline-5\nline-6",
	})

	if !expandable {
		t.Fatalf("expected tool output to be expandable")
	}

	plain := ansi.Strip(rendered)
	for _, want := range []string{"line-1", "line-2", "line-3", "line-4", toolResultExpandLabel} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected collapsed render to include %q", want)
		}
	}
	if strings.Contains(plain, "line-5") {
		t.Fatalf("expected collapsed render to hide lines after %d", toolResultPreviewLines)
	}
	if strings.Contains(plain, "REQUEST") || strings.Contains(plain, "RESPONSE") {
		t.Fatalf("expected tool widget render to avoid explicit request/response headers")
	}

	renderedExpanded, stillExpandable := m.renderToolWidget(block{
		Kind:               blockToolWidget,
		ToolArgs:           `{"query":"test"}`,
		ToolResult:         "line-1\nline-2\nline-3\nline-4\nline-5\nline-6",
		ToolResultExpanded: true,
	})

	if stillExpandable {
		t.Fatalf("expected expanded tool output to not expose expand prompt")
	}

	plainExpanded := ansi.Strip(renderedExpanded)
	if !strings.Contains(plainExpanded, "line-6") {
		t.Fatalf("expected expanded render to include all lines")
	}
	if strings.Contains(plainExpanded, toolResultExpandLabel) {
		t.Fatalf("expected expanded render to hide expand label")
	}
}

func TestExpandToolResultAtClick(t *testing.T) {
	m := newTestModel()
	m.width = 120
	m.height = 40
	m.blocks = []block{{
		Kind:       blockToolWidget,
		ToolArgs:   "{}",
		ToolResult: "line-1\nline-2\nline-3\nline-4\nline-5",
	}}

	m.relayout(true)
	if len(m.toolExpandTargets) != 1 {
		t.Fatalf("expected one expand target, got %d", len(m.toolExpandTargets))
	}

	target := m.toolExpandTargets[0]
	startX, startY := m.viewportContentOrigin()
	msg := tea.MouseMsg{
		X:      startX + target.ColStart,
		Y:      startY + target.Line - m.vp.YOffset,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}

	if !m.expandToolResultAt(msg) {
		t.Fatalf("expected click on expand label to expand tool output")
	}
	if !m.blocks[0].ToolResultExpanded {
		t.Fatalf("expected tool output block to be marked expanded")
	}

	m.refreshTranscript()
	if len(m.toolExpandTargets) != 0 {
		t.Fatalf("expected expand target to disappear after expanding")
	}
}

func TestTruncateLines(t *testing.T) {
	if got, truncated := truncateLines("", 3); got != "" || truncated {
		t.Fatalf("expected empty input to remain empty and not truncate")
	}

	if got, truncated := truncateLines("one\ntwo", 4); got != "one\ntwo" || truncated {
		t.Fatalf("expected short input unchanged, got=%q truncated=%v", got, truncated)
	}

	got, truncated := truncateLines("one\ntwo\nthree\nfour\nfive", 3)
	if got != "one\ntwo\nthree" {
		t.Fatalf("unexpected truncated text: %q", got)
	}
	if !truncated {
		t.Fatal("expected truncation flag to be true")
	}

	got, truncated = truncateLines("one\ntwo", 0)
	if got != "" || !truncated {
		t.Fatalf("expected zero maxLines to truncate non-empty input, got=%q truncated=%v", got, truncated)
	}
}

func TestFormatToolArgs(t *testing.T) {
	if got := formatToolArgs("   "); got != "{}" {
		t.Fatalf("expected whitespace-only input to become {}: %q", got)
	}

	if got := formatToolArgs("a  b\n c"); got != "a b c" {
		t.Fatalf("expected compacted invalid json: %q", got)
	}

	got := formatToolArgs(`{"query":"test","limit":5}`)
	if got != "{\n  \"query\": \"test\",\n  \"limit\": 5\n}" {
		t.Fatalf("unexpected formatted json: %q", got)
	}
}

func TestLocateToolExpandTargets(t *testing.T) {
	content := "summary line\none [expand] details\ntwo [expand]\n"
	targets := locateToolExpandTargets(content, []int{1, 2})
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}

	if targets[0].Line != 1 || targets[0].ColStart != 4 {
		t.Fatalf("unexpected first target: %#v", targets[0])
	}
	if targets[0].ColEnd-targets[0].ColStart != len(toolResultExpandLabel) {
		t.Fatalf("expected first target length to match label")
	}
	if targets[1].Line != 2 || targets[1].ColStart != 4 || targets[1].ColEnd-targets[1].ColStart != len(toolResultExpandLabel) {
		t.Fatalf("unexpected second target: %#v", targets[1])
	}
}

func TestLocateToolExpandTargetsWithNoContent(t *testing.T) {
	if got := locateToolExpandTargets("", []int{1}); len(got) != 0 {
		t.Fatalf("expected no targets for empty content, got %d", len(got))
	}
	if got := locateToolExpandTargets("plain", nil); len(got) != 0 {
		t.Fatalf("expected no targets for nil indexes, got %d", len(got))
	}
}

func TestRenderToolWidgetCompletedEmptyResultShowsPlaceholder(t *testing.T) {
	m := newTestModel()
	m.vp.Width = 120

	rendered, expandable := m.renderToolWidget(block{
		Kind:               blockToolWidget,
		ToolArgs:           `{"query":"test"}`,
		ToolResult:         "",
		ToolResultReceived: true,
		ToolState:          toolExecutionDone,
	})

	if expandable {
		t.Fatalf("expected empty output to not be expandable")
	}

	plain := ansi.Strip(rendered)
	if !strings.Contains(plain, "(empty output)") {
		t.Fatalf("expected empty output placeholder in render: %q", plain)
	}
	if strings.Contains(plain, "Running") {
		t.Fatalf("did not expect pending spinner label for completed empty output")
	}
}

func TestRenderToolWidgetPendingWithoutResultShowsRunning(t *testing.T) {
	m := newTestModel()
	m.vp.Width = 120

	rendered, expandable := m.renderToolWidget(block{
		Kind:      blockToolWidget,
		ToolArgs:  `{"query":"test"}`,
		ToolState: toolExecutionPending,
	})

	if expandable {
		t.Fatalf("expected pending output to not be expandable")
	}

	plain := ansi.Strip(rendered)
	if !strings.Contains(plain, "Running") {
		t.Fatalf("expected pending spinner label in render: %q", plain)
	}
}

func TestRenderToolWidgetPendingWithResultTextTreatsAsCompleted(t *testing.T) {
	m := newTestModel()
	m.vp.Width = 120

	rendered, expandable := m.renderToolWidget(block{
		Kind:       blockToolWidget,
		ToolArgs:   `{"query":"test"}`,
		ToolResult: "ok",
		ToolState:  toolExecutionPending,
	})

	if expandable {
		t.Fatalf("expected short output to not be expandable")
	}

	plain := ansi.Strip(rendered)
	if !strings.Contains(plain, "ok") {
		t.Fatalf("expected rendered output to include result text: %q", plain)
	}
	if strings.Contains(plain, "Running") {
		t.Fatalf("did not expect pending spinner label when result text exists")
	}
}
