package simple

import (
	"os"

	"golang.org/x/term"
)

func IsTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

func TerminalWidth(file *os.File) int {
	if !IsTerminal(file) {
		return 0
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil || width <= 0 {
		return 0
	}
	return width
}
