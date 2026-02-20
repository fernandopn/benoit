package simple

import (
	"bufio"
	"fmt"
	"strings"
)

func WriteHeader(writer *bufio.Writer, theme Theme, title string, hint string, width int) {
	if writer == nil {
		return
	}
	fmt.Fprintln(writer, theme.Style(title, theme.Bold, theme.FGAccent))
	if width > 0 {
		hint = strings.TrimSpace(hint)
		if hint != "" {
			fmt.Fprintln(writer, theme.Style(hint, theme.Dim, theme.FGMuted))
		}
	}
	fmt.Fprintln(writer)
}

func WriteToolCard(writer *bufio.Writer, theme Theme, width int, toolName, args, body string) {
	if writer == nil {
		return
	}

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
	fmt.Fprintln(writer, theme.Style(border, theme.FGMuted))

	headerLine := toolName + " (" + args + ")"
	for _, line := range wrapToWidth(headerLine, inner) {
		fmt.Fprintf(writer, "%s%s%s\n",
			theme.Style("| ", theme.FGMuted),
			theme.Style(padLine(line, inner), theme.FGAccent, theme.Bold),
			theme.Style(" |", theme.FGMuted),
		)
	}

	for _, raw := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		for _, line := range wrapToWidth(raw, inner) {
			fmt.Fprintf(writer, "%s%s%s\n",
				theme.Style("| ", theme.FGMuted),
				theme.Style(padLine(line, inner), theme.FGMuted),
				theme.Style(" |", theme.FGMuted),
			)
		}
	}

	fmt.Fprintln(writer, theme.Style(border, theme.FGMuted))
}

func PrintIncomingMessage(writer *bufio.Writer, theme Theme, sender string, text string) {
	PrintLine(writer, theme, sender, theme.Bold, theme.FGAccent)
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		PrintLine(writer, theme, line, theme.FGUser)
	}
	PrintLine(writer, theme, "", theme.Dim, theme.FGMuted)
}

func PrintOutgoingMessage(writer *bufio.Writer, theme Theme, recipient string, text string) {
	PrintLine(writer, theme, "you -> "+recipient, theme.Bold, theme.FGMuted)
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		PrintLine(writer, theme, line, theme.FGStrong)
	}
	PrintLine(writer, theme, "", theme.Dim, theme.FGMuted)
}

func PrintLine(writer *bufio.Writer, theme Theme, line string, styles ...string) {
	if writer == nil {
		return
	}
	ClearLine(writer)
	if line == "" {
		fmt.Fprint(writer, "\r\n")
		writer.Flush()
		return
	}
	fmt.Fprint(writer, theme.Style(line, styles...), "\r\n")
	writer.Flush()
}

func ClearLine(writer *bufio.Writer) {
	if writer == nil {
		return
	}
	fmt.Fprint(writer, "\r\x1b[2K")
}

func FormatContextLeft(left float64) string {
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

func compactWhitespace(value string) string {
	if value == "" {
		return ""
	}
	return strings.Join(strings.Fields(value), " ")
}
