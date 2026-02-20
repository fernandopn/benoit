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
