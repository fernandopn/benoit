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

		resp, reqErr := client.Responses.New(ctx, params)
		cancel()
		if reqErr != nil {
			fmt.Fprintln(os.Stderr, "request error:", reqErr)
			if err == io.EOF {
				return
			}
			continue
		}

		previousID = resp.ID
		output := strings.TrimSpace(resp.OutputText())
		if output == "" {
			output = "(no content)"
		}
		fmt.Printf("assistant> %s\n", output)

		if err == io.EOF {
			return
		}
	}
}
