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

	"github.com/openai/openai-go/v3"
	"github.com/fernandopn/benoid/providers"
)

func main() {
	providerName := flag.String("provider", "StreamingOpenAI", "provider class (StreamingOpenAI or DirectOpenAI)")
	flag.Parse()

	if _, ok := os.LookupEnv("OPENAI_API_KEY"); !ok {
		fmt.Fprintln(os.Stderr, "OPENAI_API_KEY is not set")
		os.Exit(1)
	}

	client := openai.NewClient()

	var provider providers.Provider
	switch strings.ToLower(strings.TrimSpace(*providerName)) {
	case "streamingopenai", "streaming":
		provider = providers.NewStreamingOpenAI(client)
	case "directopenai", "direct":
		provider = providers.NewDirectOpenAI(client)
	default:
		fmt.Fprintln(os.Stderr, "unknown provider:", *providerName)
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	fmt.Println("GPT-5.2 TUI. Type /exit to quit.")

	var previousID string
	for {
		fmt.Print("you> ")
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

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		fmt.Fprint(writer, "assistant> ")
		writer.Flush()

		newPreviousID, reqErr := provider.Chat(ctx, text, previousID, writer)
		cancel()

		if reqErr != nil {
			fmt.Fprintln(writer)
			writer.Flush()
			fmt.Fprintln(os.Stderr, "request error:", reqErr)
			if err == io.EOF {
				return
			}
			continue
		}

		fmt.Fprintln(writer)
		writer.Flush()

		previousID = newPreviousID

		if err == io.EOF {
			return
		}
	}
}
