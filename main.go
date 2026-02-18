package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/fernandopn/benoid/providers"
	"github.com/fernandopn/benoid/tools"
)

func main() {
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
		provider, err = providers.NewStreamingOpenAI(ctx, *model, toolSet)
	case "directopenai":
		provider, err = providers.NewDirectOpenAI(ctx, *model, toolSet)
	default:
		fmt.Fprintln(os.Stderr, "unknown provider:", *providerName)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "provider init error:", err)
		os.Exit(1)
	}
	cancel()

	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	fmt.Printf("%s. Type /exit to quit.\n", provider.Name())

	for {
		fmt.Print(">: ")
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			fmt.Fprintln(os.Stderr, "read error:", err)
			return
		}

		text := strings.TrimSpace(line)
		if text == "" {
			if err == io.EOF {
				fmt.Println()
				return
			}
			continue
		}
		if text == "/exit" || text == "/quit" {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), *timeout)

		var (
			hadError            bool
			messageTypeHandling *providers.MsgType
		)
		formatToolArgs := func(raw string) string {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				return ""
			}
			return "args=" + raw
		}
		writeIndented := func(text string) {
			text = strings.TrimRight(text, "\n")
			if text == "" {
				return
			}
			for _, line := range strings.Split(text, "\n") {
				fmt.Fprintf(writer, "  %s\n", line)
			}
		}
		switchState := func(next providers.MsgType) {
			if messageTypeHandling != nil && *messageTypeHandling == next {
				return
			}
			if messageTypeHandling != nil {
				fmt.Fprintln(writer)
			}
			tt := next
			messageTypeHandling = &tt
			switch next {
			case providers.MsgTypeChat:
				fmt.Fprint(writer, "[assistant]\n")
			}
		}
		msgs := provider.Chat(ctx, text)
		for msg := range msgs {
			switch msg.Type {
			case providers.MsgTypeChat:
				switchState(providers.MsgTypeChat)
				fmt.Fprint(writer, msg.Value)
				writer.Flush()
			case providers.MsgTypeError:
				hadError = true
				fmt.Fprintln(os.Stderr, "request error:", msg.Value)
			case providers.MsgTypeToolCall:
				switchState(providers.MsgTypeToolCall)
				args := formatToolArgs(msg.Value)
				if args != "" {
					fmt.Fprintf(writer, "tool call: %s %s id=%s\n", msg.Metadata["tool"], args, msg.Metadata["call_id"])
				} else {
					fmt.Fprintf(writer, "tool call: %s id=%s\n", msg.Metadata["tool"], msg.Metadata["call_id"])
				}
				writer.Flush()
			case providers.MsgTypeToolResult:
				switchState(providers.MsgTypeToolResult)
				fmt.Fprintf(writer, "tool response: %s id=%s\n", msg.Metadata["tool"], msg.Metadata["call_id"])
				writeIndented(msg.Value)
				writer.Flush()
			case providers.MsgTypeContextUsage:
				switchState(providers.MsgTypeContextUsage)
				used := msg.Metadata["tokens_used"]
				available := msg.Metadata["tokens_available"]
				if used != "" && available != "" {
					fmt.Fprintf(writer, "context usage: %s tokens_used=%s tokens_available=%s\n", msg.Value, used, available)
				} else {
					fmt.Fprintf(writer, "context usage: %s\n", msg.Value)
				}
				writer.Flush()
			}
		}
		cancel()

		if messageTypeHandling != nil {
			fmt.Fprintln(writer)
			writer.Flush()
		}

		if hadError && err == io.EOF {
			return
		}

		if err == io.EOF {
			return
		}
	}
}
