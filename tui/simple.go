package tui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/fernandopn/benoit/providers"
	"github.com/fernandopn/benoit/session"
	tuicmd "github.com/fernandopn/benoit/tui/commands"
	simpleui "github.com/fernandopn/benoit/tui/simple"
	tuiutils "github.com/fernandopn/benoit/tui/utils"
	"golang.org/x/term"
)

type simpleTheme = simpleui.Theme

type pendingToolCall struct {
	name string
	args string
}

func RunSimple(ctx context.Context, provider providers.Provider, timeout time.Duration, sessionID string) {
	if ctx == nil {
		fmt.Fprintln(os.Stderr, "context error: context is required")
		return
	}
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	colors := newSimpleTheme(term.IsTerminal(int(os.Stdout.Fd())))
	width := simpleTerminalWidth()
	sessionID = session.ResolveTUISessionID(sessionID)
	start := streamStartForProvider(provider, sessionID)

	writeSimpleHeader(writer, colors, provider.Name(), width)
	writer.Flush()

	for {
		fmt.Fprint(writer, colors.Style(">: ", colors.Bold, colors.FGAccent))
		writer.Flush()

		line, err := readSimpleInput(reader, writer)
		if err != nil && err != io.EOF {
			fmt.Fprintln(os.Stderr, "read error:", err)
			return
		}

		text := strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(text) == "" {
			if err == io.EOF {
				fmt.Println()
				return
			}
			continue
		}
		if tuicmd.IsExit(text) {
			return
		}

		writer.Flush()

		var (
			hadError bool
			section  *providers.MsgType
		)

		switchState := func(next providers.MsgType) {
			if section != nil && *section == next {
				return
			}
			if section != nil {
				fmt.Fprintln(writer)
			}
			tt := next
			section = &tt
		}

		_, streamErr := streamPrompt(ctx, text, timeout, start, streamCallbacks{
			OnChat: func(value string) {
				switchState(providers.MsgTypeChatDelta)
				fmt.Fprint(writer, colors.Style(value, colors.FGStrong))
				writer.Flush()
			},
			OnReasoning: func(value string) {
				switchState(providers.MsgTypeReasoningSummaryDelta)
				fmt.Fprint(writer, colors.Style(value, colors.FGMuted, colors.Dim))
				writer.Flush()
			},
			OnToolCall: func(name string, args string, callID string) {
				_ = callID
				switchState(providers.MsgTypeToolCall)
				writeSimpleToolCard(writer, colors, width, name, args, "Running...")
				writer.Flush()
			},
			OnToolResult: func(name string, args string, output string, callID string) {
				_ = callID
				switchState(providers.MsgTypeToolResult)
				writeSimpleToolCard(writer, colors, width, name, args, output)
				writer.Flush()
			},
			OnContextUsage: func(value string, metadata map[string]string) {
				switchState(providers.MsgTypeContextUsage)
				if left, ok := tuiutils.ContextLeftPercent(value, metadata); ok {
					fmt.Fprintln(writer, colors.Style(formatContextLeft(left), colors.FGAccent, colors.Dim))
				}
				writer.Flush()
			},
			OnCompressionStatus: func(value string, metadata map[string]string) {
				_ = metadata
				switchState(providers.MsgTypeCompressionStatus)
				fmt.Fprintln(writer, colors.Style(value, colors.FGAccent, colors.Dim))
				writer.Flush()
			},
			OnError: func(errText string) {
				hadError = true
				fmt.Fprintln(os.Stderr, colors.Style("request error:", colors.Bold, colors.FGWarn), errText)
			},
		})
		if streamErr != nil && !hadError {
			hadError = true
			fmt.Fprintln(os.Stderr, colors.Style("request error:", colors.Bold, colors.FGWarn), streamErr)
		}

		if section != nil {
			fmt.Fprintln(writer)
		}
		writer.Flush()

		if hadError && err == io.EOF {
			return
		}

		if err == io.EOF {
			return
		}
	}
}

func newSimpleTheme(enabled bool) simpleTheme {
	return simpleui.NewTheme(enabled)
}

func simpleTerminalWidth() int {
	return simpleui.TerminalWidth(os.Stdout)
}

func writeSimpleHeader(writer *bufio.Writer, colors simpleTheme, providerName string, width int) {
	title := "Benoit · " + providerName
	hint := "Enter to send | See commands / + <tab>"
	simpleui.WriteHeader(writer, colors, title, hint, width)
}

func readSimpleInput(reader *bufio.Reader, writer *bufio.Writer) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return reader.ReadString('\n')
	}
	return readSimpleInputRaw(reader, writer)
}

func readSimpleInputRaw(reader *bufio.Reader, writer *bufio.Writer) (string, error) {
	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, state)

	input := make([]rune, 0, 128)
	justInsertedNewline := false

	printNewline := func() {
		fmt.Fprint(writer, "\r\n")
		writer.Flush()
	}

	for {
		b, err := reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				if len(input) > 0 {
					printNewline()
					return string(input), io.EOF
				}
			}
			return "", err
		}

		switch b {
		case '\r', '\n':
			if justInsertedNewline {
				justInsertedNewline = false
				continue
			}
			printNewline()
			return string(input), nil
		case 0x03:
			printNewline()
			return "", io.EOF
		case 0x04:
			printNewline()
			if len(input) == 0 {
				return "", io.EOF
			}
			return string(input), nil
		case 0x08, 0x7f:
			if len(input) == 0 {
				continue
			}
			input = input[:len(input)-1]
			fmt.Fprint(writer, "\b \b")
			writer.Flush()
		case 0x1b:
			seq, err := readEscapeSequence(reader)
			if err != nil {
				if err == io.EOF {
					return string(input), io.EOF
				}
				return "", err
			}
			if isShiftEnterSequence(seq) {
				input = append(input, '\n')
				printNewline()
				justInsertedNewline = true
			}
		default:
			justInsertedNewline = false
			if b < 0x20 && b != '\t' {
				continue
			}
			r, raw, err := decodeRuneFromFirstByte(reader, b)
			if err != nil {
				return "", err
			}
			input = append(input, r)
			if _, err := writer.Write(raw); err != nil {
				return "", err
			}
			writer.Flush()
		}
	}
}

func readEscapeSequence(reader *bufio.Reader) (string, error) {
	return simpleui.ReadEscapeSequence(reader)
}

func isShiftEnterSequence(seq string) bool {
	return simpleui.IsShiftEnterSequence(seq)
}

func decodeRuneFromFirstByte(reader *bufio.Reader, first byte) (rune, []byte, error) {
	return simpleui.DecodeRuneFromFirstByteRaw(reader, first)
}

func writeSimpleToolCard(writer *bufio.Writer, colors simpleTheme, width int, toolName, args, body string) {
	simpleui.WriteToolCard(writer, colors, width, toolName, args, body)
}

func formatContextLeft(left float64) string {
	return simpleui.FormatContextLeft(left)
}
