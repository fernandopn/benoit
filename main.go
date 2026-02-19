package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fernandopn/benoid/channels"
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
	const defaultTelegramPollTimeoutSeconds = 30

	defaultRoot, rootErr := os.Getwd()
	if rootErr != nil {
		fmt.Fprintln(os.Stderr, "filesystem init error:", rootErr)
		os.Exit(1)
	}
	model := flag.String("model", "gpt-5.2", "model name")
	timeout := flag.Duration("timeout", 20*time.Minute, "request timeout (e.g. 45s, 2m)")
	fsRoot := flag.String("fs-root", defaultRoot, "filesystem root")
	dbPath := flag.String("db-path", "", "sqlite db path for chat logging")
	tuiModeFlag := flag.String("tui", defaultTUIMode, "tui mode: simple, bubbletea, or telegram")
	telegramPollTimeoutSeconds := flag.Int("telegram-poll-timeout", defaultTelegramPollTimeoutSeconds, "telegram getUpdates long poll timeout in seconds")
	flag.Parse()

	var (
		provider providers.Provider
		mode     tuiMode
		err      error
	)
	mode, err = parseTUIMode(*tuiModeFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "flag error:", err)
		os.Exit(1)
	}
	if *telegramPollTimeoutSeconds < 0 {
		fmt.Fprintln(os.Stderr, "flag error: -telegram-poll-timeout cannot be negative")
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
	if mode == tuiModeTelegram {
		telegramClient, err := channels.NewTelegramFromEnv(http.DefaultClient)
		if err != nil {
			fmt.Fprintln(os.Stderr, "telegram init error:", err)
			os.Exit(1)
		}
		if err := tui.RunTelegram(tuiCtx, telegramClient, provider, *timeout, *telegramPollTimeoutSeconds); err != nil {
			fmt.Fprintln(os.Stderr, "telegram tui error:", err)
			os.Exit(1)
		}
	} else {
		useSimpleMode := mode == tuiModeSimple
		if err := tui.Run(tuiCtx, provider, *timeout, useSimpleMode); err != nil {
			fmt.Fprintln(os.Stderr, "tui error:", err)
			os.Exit(1)
		}
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
		// {
		// 	factory:    func(_ tools.FileSystem) tools.Tool { return tools.NewClockTool() },
		// 	requiresFS: false,
		// },
		{
			factory:    func(_ tools.FileSystem) tools.Tool { return tools.NewOpenAICodeInterpreterTool() },
			requiresFS: false,
		},
		{
			factory:    func(_ tools.FileSystem) tools.Tool { return tools.NewOpenAIWebSearchTool() },
			requiresFS: false,
		},
		// {
		// 	factory:    func(fs tools.FileSystem) tools.Tool { return tools.NewListFilesToolWithFS(fs) },
		// 	requiresFS: true,
		// },
		// {
		// 	factory:    func(fs tools.FileSystem) tools.Tool { return tools.NewCurrentDirectoryToolWithFS(fs) },
		// 	requiresFS: true,
		// },
		{
			factory:    func(_ tools.FileSystem) tools.Tool { return tools.NewMatonGCalendarTool() },
			requiresFS: false,
		},
		{
			factory:    func(_ tools.FileSystem) tools.Tool { return tools.NewMatonGmailTool() },
			requiresFS: false,
		},
		// {
		// 	factory:    func(fs tools.FileSystem) tools.Tool { return tools.NewReadFileToolWithFS(fs) },
		// 	requiresFS: true,
		// },
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

type tuiMode int

const (
	tuiModeSimple tuiMode = iota
	tuiModeBubbleTea
	tuiModeTelegram
)

func parseTUIMode(raw string) (tuiMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "simple":
		return tuiModeSimple, nil
	case "bubbletea":
		return tuiModeBubbleTea, nil
	case "telegram":
		return tuiModeTelegram, nil
	default:
		return tuiModeSimple, fmt.Errorf("invalid -tui value %q (use simple, bubbletea, or telegram)", raw)
	}
}
