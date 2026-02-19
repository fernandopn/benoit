package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fernandopn/benoid/middleware"
	"github.com/fernandopn/benoid/providers"
	"github.com/fernandopn/benoid/tools"
	"github.com/fernandopn/benoid/tui"
	"github.com/openai/openai-go/v3/shared"
)

func main() {
	const OPENAI_REASONING_EFFORT = shared.ReasoningEffortHigh
	const OPENAI_REASONING_SUMMARY = shared.ReasoningSummaryDetailed
	const defaultTUIMode = "simple"

	defaultRoot, rootErr := os.Getwd()
	if rootErr != nil {
		fmt.Fprintln(os.Stderr, "filesystem init error:", rootErr)
		os.Exit(1)
	}
	model := flag.String("model", "gpt-5.2", "model name")
	timeout := flag.Duration("timeout", 20*time.Minute, "request timeout (e.g. 45s, 2m)")
	fsRoot := flag.String("fs-root", defaultRoot, "filesystem root")
	dbPath := flag.String("db-path", "", "sqlite db path for chat logging")
	tuiMode := flag.String("tui", defaultTUIMode, "tui mode: simple or bubbletea")
	flag.Parse()

	var (
		provider      providers.Provider
		useSimpleMode bool
		err           error
	)
	useSimpleMode, err = parseTUIMode(*tuiMode)
	if err != nil {
		fmt.Fprintln(os.Stderr, "flag error:", err)
		os.Exit(1)
	}
	openAIParams := providers.OpenAIParams{
		ReasoningEffort:  OPENAI_REASONING_EFFORT,
		ReasoningSummary: OPENAI_REASONING_SUMMARY,
	}
	initCtx, initCancel := context.WithTimeout(context.Background(), *timeout)
	defer initCancel()

	toolSet, err := selectedTools(*fsRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tool config error:", err)
		os.Exit(1)
	}
	provider, err = providers.NewOpenAI(*model, openAIParams, toolSet)
	if err != nil {
		fmt.Fprintln(os.Stderr, "provider init error:", err)
		os.Exit(1)
	}

	var closeMiddleware func() error
	provider, closeMiddleware, err = middleware.ConfigureSQLiteSave(initCtx, provider, strings.TrimSpace(*dbPath))
	if err != nil {
		fmt.Fprintln(os.Stderr, "middleware init error:", err)
		os.Exit(1)
	}

	tuiCtx := context.Background()
	if err := tui.Run(tuiCtx, provider, *timeout, useSimpleMode); err != nil {
		fmt.Fprintln(os.Stderr, "tui error:", err)
		os.Exit(1)
	}
	if closeMiddleware != nil {
		if err := closeMiddleware(); err != nil {
			fmt.Fprintln(os.Stderr, "middleware close error:", err)
		}
	}
}

func selectedTools(fsRoot string) ([]tools.Tool, error) {
	type toolSpec struct {
		factory    func(tools.FileSystem) tools.Tool
		requiresFS bool
	}
	allTools := []toolSpec{
		{
			factory:    func(_ tools.FileSystem) tools.Tool { return tools.NewClockTool() },
			requiresFS: false,
		},
		{
			factory:    func(fs tools.FileSystem) tools.Tool { return tools.NewListFilesToolWithFS(fs) },
			requiresFS: true,
		},
		{
			factory:    func(fs tools.FileSystem) tools.Tool { return tools.NewCurrentDirectoryToolWithFS(fs) },
			requiresFS: true,
		},
		{
			factory:    func(_ tools.FileSystem) tools.Tool { return tools.NewMatonGCalendarTool() },
			requiresFS: false,
		},
		{
			factory:    func(_ tools.FileSystem) tools.Tool { return tools.NewMatonGmailTool() },
			requiresFS: false,
		},
		{
			factory:    func(fs tools.FileSystem) tools.Tool { return tools.NewReadFileToolWithFS(fs) },
			requiresFS: true,
		},
	}

	useFS := false
	for _, spec := range allTools {
		if spec.requiresFS {
			useFS = true
			break
		}
	}

	var fs tools.FileSystem
	if useFS {
		resolvedFS, err := tools.NewRestrictedFS(strings.TrimSpace(fsRoot))
		if err != nil {
			return nil, err
		}
		fs = resolvedFS
	}

	toolSet := make([]tools.Tool, 0, len(allTools))
	for _, spec := range allTools {
		toolSet = append(toolSet, spec.factory(fs))
	}

	return toolSet, nil
}

func parseTUIMode(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "simple":
		return true, nil
	case "bubbletea":
		return false, nil
	default:
		return false, fmt.Errorf("invalid -tui value %q (use simple or bubbletea)", raw)
	}
}
