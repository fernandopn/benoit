package simple

import (
	"bufio"
	"strings"
	"unicode/utf8"
)

func ReadEscapeSequence(reader *bufio.Reader) (string, error) {
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

func DiscardEscapeSequence(reader *bufio.Reader) {
	for reader.Buffered() > 0 {
		b, err := reader.ReadByte()
		if err != nil {
			return
		}
		if b >= 0x40 && b <= 0x7e {
			return
		}
	}
}

func IsShiftEnterSequence(seq string) bool {
	if !strings.HasPrefix(seq, "\x1b[") {
		return false
	}
	nums := ExtractCSIInts(seq)
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

func ExtractCSIInts(seq string) []int {
	var nums []int
	n := 0
	inNum := false
	for _, r := range seq {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
			inNum = true
			continue
		}
		if inNum {
			nums = append(nums, n)
			n = 0
			inNum = false
		}
	}
	if inNum {
		nums = append(nums, n)
	}
	return nums
}

func DecodeRuneFromFirstByte(reader *bufio.Reader, first byte) (rune, error) {
	r, _, err := DecodeRuneFromFirstByteRaw(reader, first)
	return r, err
}

func DecodeRuneFromFirstByteRaw(reader *bufio.Reader, first byte) (rune, []byte, error) {
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
