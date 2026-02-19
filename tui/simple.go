package tui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fernandopn/benoit/providers"
	"golang.org/x/term"
)

type simpleTheme struct {
	enabled  bool
	reset    string
	bold     string
	dim      string
	fgStrong string
	fgMuted  string
	fgAccent string
	fgWarn   string
	fgUser   string
	bgUser   string
}

type pendingToolCall struct {
	name string
	args string
}

func RunSimple(provider providers.Provider, timeout time.Duration) {
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	colors := newSimpleTheme(term.IsTerminal(int(os.Stdout.Fd())))
	width := simpleTerminalWidth()

	writeSimpleHeader(writer, colors, provider.Name(), width)
	writer.Flush()

	for {
		fmt.Fprint(writer, colors.style(">: ", colors.bold, colors.fgAccent))
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
		if strings.TrimSpace(text) == "/exit" || strings.TrimSpace(text) == "/quit" {
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

		_, streamErr := streamPrompt(context.Background(), text, timeout, streamStartForProvider(provider, ""), streamCallbacks{
			OnChat: func(value string) {
				switchState(providers.MsgTypeChat)
				fmt.Fprint(writer, colors.style(value, colors.fgStrong))
				writer.Flush()
			},
			OnReasoning: func(value string) {
				switchState(providers.MsgTypeReasoningSummary)
				fmt.Fprint(writer, colors.style(value, colors.fgMuted, colors.dim))
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
				if left, ok := contextLeftPercent(value, metadata); ok {
					fmt.Fprintln(writer, colors.style(formatContextLeft(left), colors.fgAccent, colors.dim))
				}
				writer.Flush()
			},
			OnError: func(errText string) {
				hadError = true
				fmt.Fprintln(os.Stderr, colors.style("request error:", colors.bold, colors.fgWarn), errText)
			},
		})
		if streamErr != nil && !hadError {
			hadError = true
			fmt.Fprintln(os.Stderr, colors.style("request error:", colors.bold, colors.fgWarn), streamErr)
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
	return simpleTheme{
		enabled:  enabled,
		reset:    "\x1b[0m",
		bold:     "\x1b[1m",
		dim:      "\x1b[2m",
		fgStrong: ansiForeground("FFFFFF"),
		fgMuted:  ansiForeground("95A3B8"),
		fgAccent: ansiForeground("8FB3FF"),
		fgWarn:   ansiForeground("FF5F5F"),
		fgUser:   ansiForeground("E6EDF3"),
		bgUser:   ansiBackground("1C1C1C"),
	}
}

func ansiForeground(hex string) string {
	r, g, b := rgbFromHex(hex)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

func ansiBackground(hex string) string {
	r, g, b := rgbFromHex(hex)
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}

func rgbFromHex(hex string) (int64, int64, int64) {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 {
		return 255, 255, 255
	}
	r, rErr := strconv.ParseInt(hex[0:2], 16, 64)
	g, gErr := strconv.ParseInt(hex[2:4], 16, 64)
	b, bErr := strconv.ParseInt(hex[4:6], 16, 64)
	if rErr != nil || gErr != nil || bErr != nil {
		return 255, 255, 255
	}
	return r, g, b
}

func (t simpleTheme) style(text string, codes ...string) string {
	if !t.enabled || len(codes) == 0 {
		return text
	}
	return strings.Join(codes, "") + text + t.reset
}

func simpleTerminalWidth() int {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return 0
	}
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return 0
	}
	return width
}

func writeSimpleHeader(writer *bufio.Writer, colors simpleTheme, providerName string, width int) {
	left := "Benoit · " + providerName
	fmt.Fprintln(writer, colors.style(left, colors.bold, colors.fgAccent))
	if width > 0 {
		hint := "Enter to send | Shift+Enter newline | /exit to quit"
		fmt.Fprintln(writer, colors.style(hint, colors.dim, colors.fgMuted))
	}
	fmt.Fprintln(writer)
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
	next, err := reader.ReadByte()
	if err != nil {
		return "", err
	}
	buf := []byte{0x1b, next}
	if next != '[' {
		return string(buf), nil
	}

	for len(buf) < 32 {
		b, err := reader.ReadByte()
		if err != nil {
			return string(buf), err
		}
		buf = append(buf, b)
		if b >= 0x40 && b <= 0x7e {
			break
		}
	}

	return string(buf), nil
}

func isShiftEnterSequence(seq string) bool {
	if !strings.HasPrefix(seq, "\x1b[") {
		return false
	}
	nums := extractCSIInts(seq)
	hasEnter := false
	hasShift := false
	for _, n := range nums {
		if n == 13 {
			hasEnter = true
		}
		if n == 2 {
			hasShift = true
		}
	}
	return hasEnter && hasShift
}

func decodeRuneFromFirstByte(reader *bufio.Reader, first byte) (rune, []byte, error) {
	if first < utf8.RuneSelf {
		return rune(first), []byte{first}, nil
	}

	need := utf8SequenceLength(first)
	if need == 1 {
		return rune(first), []byte{first}, nil
	}

	buf := make([]byte, 0, need)
	buf = append(buf, first)
	for len(buf) < need {
		b, err := reader.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		buf = append(buf, b)
	}

	r, size := utf8.DecodeRune(buf)
	if r == utf8.RuneError && size == 1 {
		return rune(first), []byte{first}, nil
	}
	return r, buf[:size], nil
}

func utf8SequenceLength(first byte) int {
	switch {
	case first&0x80 == 0x00:
		return 1
	case first&0xe0 == 0xc0:
		return 2
	case first&0xf0 == 0xe0:
		return 3
	case first&0xf8 == 0xf0:
		return 4
	default:
		return 1
	}
}

func writeSimpleToolCard(writer *bufio.Writer, colors simpleTheme, width int, toolName, args, body string) {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		toolName = "tool"
	}
	args = compactWhitespace(strings.TrimSpace(args))
	if args == "" {
		args = "{}"
	}
	body = strings.TrimSpace(body)
	if body == "" {
		body = "(empty output)"
	}

	cardWidth := width
	if cardWidth <= 0 {
		cardWidth = 88
	}
	if cardWidth > 100 {
		cardWidth = 100
	}
	if cardWidth < 40 {
		cardWidth = 40
	}

	inner := cardWidth - 4
	border := "+" + strings.Repeat("-", cardWidth-2) + "+"
	fmt.Fprintln(writer, colors.style(border, colors.fgMuted))

	headerLine := toolName + " (" + args + ")"
	for _, line := range wrapToWidth(headerLine, inner) {
		fmt.Fprintf(writer, "%s%s%s\n",
			colors.style("| ", colors.fgMuted),
			colors.style(padLine(line, inner), colors.fgAccent, colors.bold),
			colors.style(" |", colors.fgMuted),
		)
	}

	for _, raw := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		for _, line := range wrapToWidth(raw, inner) {
			fmt.Fprintf(writer, "%s%s%s\n",
				colors.style("| ", colors.fgMuted),
				colors.style(padLine(line, inner), colors.fgMuted),
				colors.style(" |", colors.fgMuted),
			)
		}
	}

	fmt.Fprintln(writer, colors.style(border, colors.fgMuted))
}

func wrapToWidth(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	if text == "" {
		return []string{""}
	}
	text = strings.ReplaceAll(text, "\t", "  ")
	runes := []rune(text)
	if len(runes) <= width {
		return []string{text}
	}

	lines := make([]string, 0, (len(runes)/width)+1)
	for len(runes) > 0 {
		if len(runes) <= width {
			lines = append(lines, string(runes))
			break
		}
		lines = append(lines, string(runes[:width]))
		runes = runes[width:]
	}
	return lines
}

func padLine(text string, width int) string {
	runes := []rune(text)
	if len(runes) >= width {
		return string(runes[:width])
	}
	return text + strings.Repeat(" ", width-len(runes))
}

func formatContextLeft(left float64) string {
	if left < 0 {
		left = 0
	}
	if left > 100 {
		left = 100
	}
	if left >= 99.95 {
		return "100% of the context left."
	}
	return fmt.Sprintf("%.1f%% of the context left.", left)
}
