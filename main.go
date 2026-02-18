package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fernandopn/benoid/providers"
	"github.com/fernandopn/benoid/tools"
	"github.com/fernandopn/benoid/tui"
	"github.com/openai/openai-go/v3/shared"
)

func main() {
	const OPENAI_REASONING_EFFORT = shared.ReasoningEffortHigh
	const OPENAI_REASONING_SUMMARY = shared.ReasoningSummaryDetailed

	defaultRoot, rootErr := os.Getwd()
	if rootErr != nil {
		fmt.Fprintln(os.Stderr, "filesystem init error:", rootErr)
		os.Exit(1)
	}
	providerName := flag.String("provider", "StreamingOpenAI", "provider class (StreamingOpenAI or DirectOpenAI)")
	model := flag.String("model", "gpt-5.2", "model name")
	timeout := flag.Duration("timeout", 60*time.Second, "request timeout (e.g. 45s, 2m)")
	fsRoot := flag.String("fs-root", defaultRoot, "filesystem root")
	flag.Parse()

	var (
		provider providers.Provider
		err      error
	)
	openAIParams := providers.OpenAIParams{
		ReasoningEffort:  OPENAI_REASONING_EFFORT,
		ReasoningSummary: OPENAI_REASONING_SUMMARY,
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	fs, err := tools.NewRestrictedFS(*fsRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "filesystem init error:", err)
		os.Exit(1)
	}
	toolSet := []tools.Tool{
		tools.NewClockTool(),
		tools.NewListFilesToolWithFS(fs),
		tools.NewCurrentDirectoryToolWithFS(fs),
		tools.NewReadFileToolWithFS(fs),
	}
	switch strings.ToLower(strings.TrimSpace(*providerName)) {
	case "streamingopenai":
		provider, err = providers.NewStreamingOpenAI(ctx, *model, openAIParams, toolSet)
	case "directopenai":
		provider, err = providers.NewDirectOpenAI(ctx, *model, openAIParams, toolSet)
	default:
		fmt.Fprintln(os.Stderr, "unknown provider:", *providerName)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "provider init error:", err)
		os.Exit(1)
	}
	cancel()

	tui.RunSimple(provider, *timeout)
}
