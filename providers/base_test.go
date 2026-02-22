package providers

import (
	"context"
	"errors"
	"testing"
)

type compressionProviderStub struct {
	summary      string
	err          error
	status       Msg
	contextUsage Msg
	notifyCalled int
	notifyID     string
}

func (p *compressionProviderStub) Chat(_ context.Context, _ string) <-chan Msg {
	out := make(chan Msg)
	close(out)
	return out
}

func (p *compressionProviderStub) PerformCompression(ctx context.Context, _ string, _ Compressor) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	if p.status.Type != 0 || p.status.Value != "" || len(p.status.Metadata) > 0 {
		SetCompressionStatus(ctx, p.status)
	}
	if p.contextUsage.Type != 0 || p.contextUsage.Value != "" || len(p.contextUsage.Metadata) > 0 {
		SetContextUsage(ctx, p.contextUsage)
	}
	return p.summary, nil
}

func (p *compressionProviderStub) ListModels(_ context.Context) ([]string, error) {
	return nil, nil
}

func (p *compressionProviderStub) Name() string {
	return "stub"
}

func (p *compressionProviderStub) NotifyCompressionStatusSent(sessionID string) {
	p.notifyCalled++
	p.notifyID = sessionID
}

type noopCompressor struct{}

func (noopCompressor) Compress(context.Context, Provider, string) (string, error) {
	return "", nil
}

type noNotifyProvider struct{}

func (noNotifyProvider) Chat(context.Context, string) <-chan Msg {
	out := make(chan Msg)
	close(out)
	return out
}

func (noNotifyProvider) PerformCompression(context.Context, string, Compressor) (string, error) {
	return "", nil
}

func (noNotifyProvider) ListModels(context.Context) ([]string, error) {
	return nil, nil
}

func (noNotifyProvider) Name() string {
	return "no-notify"
}

func TestPerformCompressionWithStatus(t *testing.T) {
	provider := &compressionProviderStub{
		summary: "  compact summary  ",
		status: Msg{
			Type:  MsgTypeCompressionStatus,
			Value: "done",
		},
	}

	summary, status, contextUsage, err := PerformCompressionWithStatus(context.Background(), provider, "session-1", noopCompressor{}, "")
	if err != nil {
		t.Fatalf("PerformCompressionWithStatus unexpected error: %v", err)
	}
	if summary != "compact summary" {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if status.Type != MsgTypeCompressionStatus || status.Value != "done" {
		t.Fatalf("unexpected status: %#v", status)
	}
	if contextUsage.Type != 0 || contextUsage.Value != "" || len(contextUsage.Metadata) > 0 {
		t.Fatalf("did not expect context usage message, got %#v", contextUsage)
	}
}

func TestPerformCompressionWithStatusFallback(t *testing.T) {
	provider := &compressionProviderStub{summary: "summary"}

	_, status, contextUsage, err := PerformCompressionWithStatus(context.Background(), provider, "session-1", noopCompressor{}, "")
	if err != nil {
		t.Fatalf("PerformCompressionWithStatus unexpected error: %v", err)
	}
	if status.Type != MsgTypeCompressionStatus || status.Value != DefaultCompressionFinishedMessage {
		t.Fatalf("unexpected fallback status: %#v", status)
	}
	if contextUsage.Type != 0 || contextUsage.Value != "" || len(contextUsage.Metadata) > 0 {
		t.Fatalf("did not expect context usage message, got %#v", contextUsage)
	}
}

func TestPerformCompressionWithStatusIncludesContextUsage(t *testing.T) {
	provider := &compressionProviderStub{
		summary: "summary",
		contextUsage: Msg{
			Type:  MsgTypeContextUsage,
			Value: "6.0%",
			Metadata: map[string]string{
				"tokens_input_used": "24000",
				"tokens_available":  "400000",
			},
		},
	}

	_, _, contextUsage, err := PerformCompressionWithStatus(context.Background(), provider, "session-1", noopCompressor{}, "")
	if err != nil {
		t.Fatalf("PerformCompressionWithStatus unexpected error: %v", err)
	}
	if contextUsage.Type != MsgTypeContextUsage {
		t.Fatalf("expected context usage message, got %#v", contextUsage)
	}
	if contextUsage.Metadata["tokens_input_used"] != "24000" {
		t.Fatalf("unexpected tokens_input_used: %q", contextUsage.Metadata["tokens_input_used"])
	}
}

func TestPerformCompressionWithStatusInfersContextUsageFromStatus(t *testing.T) {
	provider := &compressionProviderStub{
		summary: "summary",
		status: Msg{
			Type:  MsgTypeCompressionStatus,
			Value: "done",
			Metadata: map[string]string{
				"to_tokens_used":      "21000",
				"to_tokens_available": "400000",
			},
		},
	}

	_, _, contextUsage, err := PerformCompressionWithStatus(context.Background(), provider, "session-1", noopCompressor{}, "")
	if err != nil {
		t.Fatalf("PerformCompressionWithStatus unexpected error: %v", err)
	}
	if contextUsage.Type != MsgTypeContextUsage {
		t.Fatalf("expected inferred context usage, got %#v", contextUsage)
	}
	if contextUsage.Metadata["tokens_input_used"] != "21000" {
		t.Fatalf("unexpected inferred tokens_input_used: %q", contextUsage.Metadata["tokens_input_used"])
	}
	if contextUsage.Metadata["tokens_available"] != "400000" {
		t.Fatalf("unexpected inferred tokens_available: %q", contextUsage.Metadata["tokens_available"])
	}
}

func TestPerformCompressionWithStatusValidation(t *testing.T) {
	provider := &compressionProviderStub{summary: "summary"}

	if _, _, _, err := PerformCompressionWithStatus(nil, provider, "", noopCompressor{}, ""); err == nil {
		t.Fatal("expected context validation error")
	}
	if _, _, _, err := PerformCompressionWithStatus(context.Background(), nil, "", noopCompressor{}, ""); err == nil {
		t.Fatal("expected provider validation error")
	}
	if _, _, _, err := PerformCompressionWithStatus(context.Background(), provider, "", nil, ""); err == nil {
		t.Fatal("expected compressor validation error")
	}
}

func TestPerformCompressionWithStatusErrors(t *testing.T) {
	provider := &compressionProviderStub{summary: "summary", err: errors.New("boom")}
	if _, _, _, err := PerformCompressionWithStatus(context.Background(), provider, "", noopCompressor{}, ""); err == nil {
		t.Fatal("expected provider error")
	}

	empty := &compressionProviderStub{summary: "  "}
	if _, _, _, err := PerformCompressionWithStatus(context.Background(), empty, "", noopCompressor{}, ""); err == nil {
		t.Fatal("expected empty summary error")
	}
}

func TestNotifyCompressionStatusSent(t *testing.T) {
	provider := &compressionProviderStub{}
	NotifyCompressionStatusSent(provider, "  session-42  ")
	if provider.notifyCalled != 1 || provider.notifyID != "session-42" {
		t.Fatalf("unexpected notify state: calls=%d id=%q", provider.notifyCalled, provider.notifyID)
	}

	NotifyCompressionStatusSent(noNotifyProvider{}, "session-1")
}
