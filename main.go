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
	"github.com/openai/openai-go/v3"
)

func main() {
	providerName := flag.String("provider", "StreamingOpenAI", "provider class (StreamingOpenAI or DirectOpenAI)")
	timeout := flag.Duration("timeout", 60*time.Second, "request timeout (e.g. 45s, 2m)")
	flag.Parse()

	if _, ok := os.LookupEnv("OPENAI_API_KEY"); !ok {
		fmt.Fprintln(os.Stderr, "OPENAI_API_KEY is not set")
		os.Exit(1)
	}

	client := openai.NewClient()

	var provider providers.Provider
	switch strings.ToLower(strings.TrimSpace(*providerName)) {
	case "streamingopenai":
		provider = providers.NewStreamingOpenAI(client)
	case "directopenai":
		provider = providers.NewDirectOpenAI(client)
	default:
		fmt.Fprintln(os.Stderr, "unknown provider:", *providerName)
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	fmt.Println("GPT-5.2 TUI. Type /exit to quit.")

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
		fmt.Fprint(writer, "Assistant> ")
		writer.Flush()

		var hadError bool
		msgs := provider.Chat(ctx, text)
		for msg := range msgs {
			switch msg.Type {
			case providers.MsgTypeChat:
				fmt.Fprint(writer, msg.Value)
				writer.Flush()
			case providers.MsgTypeError:
				hadError = true
				fmt.Fprintln(os.Stderr, "request error:", msg.Value)
			}
		}
		cancel()

		fmt.Fprintln(writer)
		writer.Flush()

		if hadError && err == io.EOF {
			return
		}

		if err == io.EOF {
			return
		}
	}
}
