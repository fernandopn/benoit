package simple

import (
	"fmt"
	"strconv"
	"strings"
)

type Theme struct {
	Enabled bool
	Reset   string
	Bold    string
	Dim     string

	FGStrong string
	FGMuted  string
	FGAccent string
	FGWarn   string
	FGUser   string
	BGUser   string
}

func NewTheme(enabled bool) Theme {
	return Theme{
		Enabled:  enabled,
		Reset:    "\x1b[0m",
		Bold:     "\x1b[1m",
		Dim:      "\x1b[2m",
		FGStrong: ansiForeground("FFFFFF"),
		FGMuted:  ansiForeground("95A3B8"),
		FGAccent: ansiForeground("8FB3FF"),
		FGWarn:   ansiForeground("FF5F5F"),
		FGUser:   ansiForeground("E6EDF3"),
		BGUser:   ansiBackground("1C1C1C"),
	}
}

func (t Theme) Style(text string, codes ...string) string {
	if !t.Enabled || len(codes) == 0 {
		return text
	}
	return strings.Join(codes, "") + text + t.Reset
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
