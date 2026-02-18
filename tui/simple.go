package tui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/fernandopn/benoid/providers"
)

func RunSimple(provider providers.Provider, timeout time.Duration) {
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	fmt.Printf("%s. Type /exit to quit.\n", provider.Name())

	const (
		ansiReset = "\x1b[0m"
		ansiDim   = "\x1b[2m"
		ansiGray  = "\x1b[90m"
		ansiBold  = "\x1b[1m"
	)

	style := func(text string, codes ...string) string {
		if len(codes) == 0 {
			return text
		}
		return strings.Join(codes, "") + text + ansiReset
	}

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

		ctx, cancel := context.WithTimeout(context.Background(), timeout)

		var (
			hadError            bool
			messageTypeHandling *providers.MsgType
		)
		writeIndented := func(text string, prefix string) {
			text = strings.TrimRight(text, "\n")
			if text == "" {
				return
			}
			for _, line := range strings.Split(text, "\n") {
				fmt.Fprintf(writer, "%s%s\n", prefix, line)
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
				fmt.Fprint(writer, style("[assistant]\n", ansiBold))
			case providers.MsgTypeReasoningSummary:
				fmt.Fprint(writer, style("[reasoning summary]\n", ansiDim, ansiGray))
			case providers.MsgTypeToolCall:
				fmt.Fprint(writer, style("[tool call]\n", ansiBold))
			case providers.MsgTypeToolResult:
				fmt.Fprint(writer, style("[tool result]\n", ansiBold))
			case providers.MsgTypeContextUsage:
				fmt.Fprint(writer, style("[context usage]\n", ansiDim, ansiGray))
			}
		}
		msgs := provider.Chat(ctx, text)
		for msg := range msgs {
			switch msg.Type {
			case providers.MsgTypeChat, providers.MsgTypeReasoningSummary:
				switchState(msg.Type)
				if msg.Type == providers.MsgTypeReasoningSummary {
					fmt.Fprint(writer, style(msg.Value, ansiDim, ansiGray))
				} else {
					fmt.Fprint(writer, msg.Value)
				}
				writer.Flush()
			case providers.MsgTypeError:
				hadError = true
				fmt.Fprintln(os.Stderr, "request error:", msg.Value)
			case providers.MsgTypeToolCall:
				switchState(providers.MsgTypeToolCall)
				writeIndented("name: "+msg.Metadata["tool"], "  ")
				writeIndented("id: "+msg.Metadata["call_id"], "  ")
				if args := strings.TrimSpace(msg.Value); args != "" {
					writeIndented("args: "+args, "  ")
				}
				writer.Flush()
			case providers.MsgTypeToolResult:
				switchState(providers.MsgTypeToolResult)
				writeIndented("name: "+msg.Metadata["tool"], "  ")
				writeIndented("id: "+msg.Metadata["call_id"], "  ")
				if output := strings.TrimSpace(msg.Value); output != "" {
					writeIndented("output:", "  ")
					writeIndented(output, "    ")
				}
				writer.Flush()
			case providers.MsgTypeContextUsage:
				switchState(providers.MsgTypeContextUsage)
				used := msg.Metadata["tokens_used"]
				available := msg.Metadata["tokens_available"]
				if used != "" && available != "" {
					fmt.Fprint(writer, style(fmt.Sprintf("usage: %s tokens_used=%s tokens_available=%s\n", msg.Value, used, available), ansiDim, ansiGray))
				} else {
					fmt.Fprint(writer, style(fmt.Sprintf("usage: %s\n", msg.Value), ansiDim, ansiGray))
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
