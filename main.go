package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func main() {
	if _, ok := os.LookupEnv("OPENAI_API_KEY"); !ok {
		fmt.Fprintln(os.Stderr, "OPENAI_API_KEY is not set")
		os.Exit(1)
	}

	client := openai.NewClient()
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
		params := responses.ResponseNewParams{
			Model: openai.ChatModelGPT5_2,
			Input: responses.ResponseNewParamsInputUnion{OfString: openai.String(text)},
		}
		if previousID != "" {
			params.PreviousResponseID = openai.String(previousID)
		}

		stream := client.Responses.NewStreaming(ctx, params)
		fmt.Fprint(writer, "assistant> ")
		writer.Flush()

		var (
			sawText     bool
			completedID string
		)

		for stream.Next() {
			event := stream.Current()
			if event.Type == "response.output_text.delta" && event.Delta != "" {
				fmt.Fprint(writer, event.Delta)
				writer.Flush()
				sawText = true
			}
			if event.Type == "response.completed" && event.Response.ID != "" {
				completedID = event.Response.ID
			}
		}
		cancel()

		if stream.Err() != nil {
			fmt.Fprintln(writer)
			writer.Flush()
			fmt.Fprintln(os.Stderr, "stream error:", stream.Err())
			if err == io.EOF {
				return
			}
			continue
		}

		if !sawText {
			fmt.Fprint(writer, "(no content)")
		}
		fmt.Fprintln(writer)
		writer.Flush()

		if completedID != "" {
			previousID = completedID
		}

		if err == io.EOF {
			return
		}
	}
}
