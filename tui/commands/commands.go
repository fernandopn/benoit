package commands

import (
	"errors"
	"strconv"
	"strings"
)

const DefaultCompressionMaxWords = 300

const (
	CompressCommand = "/compress"
	ExitCommand     = "/exit"
	QuitCommand     = "/quit"
)

const compressUsage = "usage: /compress [max_words]"

type Kind int

const (
	KindNone Kind = iota
	KindCompress
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
	case CompressCommand:
		if len(parts) == 1 {
			return Parsed{Kind: KindCompress, MaxWords: DefaultCompressionMaxWords}, nil
		}
		if len(parts) != 2 {
			return Parsed{Kind: KindCompress}, errors.New(compressUsage)
		}
		maxWords, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || maxWords <= 0 {
			return Parsed{Kind: KindCompress}, errors.New(compressUsage)
		}
		return Parsed{Kind: KindCompress, MaxWords: maxWords}, nil
	case ExitCommand, QuitCommand:
		if len(parts) == 1 {
			return Parsed{Kind: KindExit}, nil
		}
		return Parsed{Kind: KindNone}, nil
	default:
		return Parsed{Kind: KindNone}, nil
	}
}

func ParseCompress(input string) (int, bool, error) {
	parsed, err := Parse(input)
	if parsed.Kind != KindCompress {
		return 0, false, nil
	}
	return parsed.MaxWords, true, err
}

func IsExit(input string) bool {
	parsed, err := Parse(input)
	if err != nil {
		return false
	}
	return parsed.Kind == KindExit
}
