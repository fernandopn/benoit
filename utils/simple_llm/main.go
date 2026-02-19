package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fernandopn/benoit/providers"
	"golang.org/x/term"
)

const (
	defaultModel   = "gpt-5.2"
	defaultTimeout = 20 * time.Minute

	openAIAPIKeyEnv = "OPENAI_API_KEY"
)

type config struct {
	model   string
	timeout time.Duration
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := loadConfig(args)
	if err != nil {
		return err
	}

	apiKey := strings.TrimSpace(os.Getenv(openAIAPIKeyEnv))
	if apiKey == "" {
		return fmt.Errorf("%s is not set", openAIAPIKeyEnv)
	}

	provider, err := providers.NewOpenAI(cfg.model, apiKey, providers.OpenAIParams{}, nil)
	if err != nil {
		return err
	}

	reader := bufio.NewScanner(os.Stdin)
	reader.Buffer(make([]byte, 0, 1024), 1024*1024)

	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()

	interactive := term.IsTerminal(int(os.Stdin.Fd()))
	for {
		if interactive {
			fmt.Fprint(writer, "> ")
			writer.Flush()
		}

		if !reader.Scan() {
			if err := reader.Err(); err != nil {
				return err
			}
			return nil
		}

		input := strings.TrimSpace(reader.Text())
		if input == "" {
			continue
		}
		if input == "/exit" || input == "/quit" {
			return nil
		}

		requestCtx := context.Background()
		cancel := func() {}
		if cfg.timeout > 0 {
			requestCtx, cancel = context.WithTimeout(requestCtx, cfg.timeout)
		}
		response, err := finalMessage(requestCtx, provider, input)
		cancel()
		if err != nil {
			return err
		}

		fmt.Fprintln(writer, response)
		writer.Flush()
	}
}

func loadConfig(args []string) (config, error) {
	flagSet := flag.NewFlagSet("simple-llm", flag.ContinueOnError)
	model := flagSet.String("model", defaultModel, "model name")
	timeout := flagSet.Duration("timeout", defaultTimeout, "request timeout (e.g. 45s, 2m)")
	if err := flagSet.Parse(args); err != nil {
		return config{}, err
	}
	if len(flagSet.Args()) > 0 {
		return config{}, errors.New("unexpected positional arguments")
	}
	if *timeout < 0 {
		return config{}, errors.New("timeout cannot be negative")
	}

	return config{model: strings.TrimSpace(*model), timeout: *timeout}, nil
}

func finalMessage(ctx context.Context, provider providers.Provider, input string) (string, error) {
	stream := provider.Chat(ctx, input)
	var (
		finalOutput   strings.Builder
		deltaFallback strings.Builder
	)

	for message := range stream {
		switch message.Type {
		case providers.MsgTypeChatFinal:
			finalOutput.WriteString(message.Value)
		case providers.MsgTypeChatDelta:
			deltaFallback.WriteString(message.Value)
		case providers.MsgTypeError:
			errText := strings.TrimSpace(message.Value)
			if errText == "" {
				errText = "provider error"
			}
			return "", errors.New(errText)
		}
	}

	output := finalOutput.String()
	if strings.TrimSpace(output) == "" {
		output = deltaFallback.String()
	}
	return output, nil
}
