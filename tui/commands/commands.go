package commands

import (
	"errors"
	"strconv"
	"strings"
)

const DefaultCompressionMaxWords = 300

const (
	CompactCommand = "/compact"
	// CompressCommand is kept for compatibility with existing imports.
	CompressCommand = CompactCommand
	ExitCommand     = "/exit"
	QuitCommand     = "/quit"
)

const compactUsage = "usage: /compact [max_words]"

type Kind int

const (
	KindNone Kind = iota
	KindCompact
	KindExit
)

type Parsed struct {
	Kind     Kind
	MaxWords int
}

func Parse(input string) (Parsed, error) {
	parts := strings.Fields(strings.TrimSpace(input))
	if len(parts) == 0 {
		return Parsed{Kind: KindNone}, nil
	}

	switch strings.ToLower(parts[0]) {
	case CompactCommand:
		if len(parts) == 1 {
			return Parsed{Kind: KindCompact, MaxWords: DefaultCompressionMaxWords}, nil
		}
		if len(parts) != 2 {
			return Parsed{Kind: KindCompact}, errors.New(compactUsage)
		}
		maxWords, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || maxWords <= 0 {
			return Parsed{Kind: KindCompact}, errors.New(compactUsage)
		}
		return Parsed{Kind: KindCompact, MaxWords: maxWords}, nil
	case ExitCommand, QuitCommand:
		if len(parts) == 1 {
			return Parsed{Kind: KindExit}, nil
		}
		return Parsed{Kind: KindNone}, nil
	default:
		return Parsed{Kind: KindNone}, nil
	}
}

func ParseCompact(input string) (int, bool, error) {
	parsed, err := Parse(input)
	if parsed.Kind != KindCompact {
		return 0, false, nil
	}
	return parsed.MaxWords, true, err
}

// ParseCompress is kept for compatibility with existing callers.
func ParseCompress(input string) (int, bool, error) {
	return ParseCompact(input)
}

func IsExit(input string) bool {
	parsed, err := Parse(input)
	if err != nil {
		return false
	}
	return parsed.Kind == KindExit
}
