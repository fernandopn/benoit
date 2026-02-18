package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
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

	defaultRoot, rootErr := os.Getwd()
	if rootErr != nil {
		fmt.Fprintln(os.Stderr, "filesystem init error:", rootErr)
		os.Exit(1)
	}
	model := flag.String("model", "gpt-5.2", "model name")
	timeout := flag.Duration("timeout", 60*time.Second, "request timeout (e.g. 45s, 2m)")
	fsRoot := flag.String("fs-root", defaultRoot, "filesystem root")
	dbPath := flag.String("db-path", "", "sqlite db path for chat logging")
	simpleMode := flag.Bool("simple", false, "use simple line-based interface")
	disableTools := flag.Bool("no-tools", false, "disable tool usage")
	toolsList := flag.String("tools", "", "comma-separated tools to enable when tools are allowed. options: clock,list_files,get_current_directory,read_file (default: all)")
	flag.Parse()

	var (
		provider providers.Provider
		err      error
	)
	openAIParams := providers.OpenAIParams{
		ReasoningEffort:  OPENAI_REASONING_EFFORT,
		ReasoningSummary: OPENAI_REASONING_SUMMARY,
	}
	initCtx, initCancel := context.WithTimeout(context.Background(), *timeout)
	defer initCancel()

	toolSet, err := selectedTools(*disableTools, *toolsList, *fsRoot)
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
	if err := tui.Run(tuiCtx, provider, *timeout, *simpleMode); err != nil {
		fmt.Fprintln(os.Stderr, "tui error:", err)
		os.Exit(1)
	}
	if closeMiddleware != nil {
		if err := closeMiddleware(); err != nil {
			fmt.Fprintln(os.Stderr, "middleware close error:", err)
		}
	}
}
func selectedTools(noTools bool, toolsArg, fsRoot string) ([]tools.Tool, error) {
	if noTools {
		return nil, nil
	}

	requested := map[string]bool{}
	allTools := map[string]func(tools.FileSystem) tools.Tool{
		"clock":                 func(_ tools.FileSystem) tools.Tool { return tools.NewClockTool() },
		"list_files":            func(fs tools.FileSystem) tools.Tool { return tools.NewListFilesToolWithFS(fs) },
		"get_current_directory": func(fs tools.FileSystem) tools.Tool { return tools.NewCurrentDirectoryToolWithFS(fs) },
		"read_file":             func(fs tools.FileSystem) tools.Tool { return tools.NewReadFileToolWithFS(fs) },
	}

	var useFS bool
	if strings.TrimSpace(toolsArg) == "" {
		for name := range allTools {
			requested[name] = true
		}
	} else {
		for _, raw := range strings.Split(toolsArg, ",") {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			if _, ok := allTools[name]; !ok {
				return nil, fmt.Errorf("unknown tool: %s", name)
			}
			requested[name] = true
		}
		if len(requested) == 0 {
			return nil, nil
		}
	}

	for name := range requested {
		if name != "clock" {
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

	names := make([]string, 0, len(requested))
	for name := range requested {
		names = append(names, name)
	}
	sort.Strings(names)

	toolSet := make([]tools.Tool, 0, len(names))
	for _, name := range names {
		if factory, ok := allTools[name]; ok {
			toolSet = append(toolSet, factory(fs))
		}
	}

	return toolSet, nil
}
