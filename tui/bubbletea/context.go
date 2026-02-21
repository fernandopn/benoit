package bubbletea

import (
	"strconv"
	"strings"
)

func contextLeftPercent(value string, meta map[string]string) (float64, bool) {
	if percentUsed, ok := parsePercent(value); ok {
		return 100 - percentUsed, true
	}
	if meta != nil {
		usedText := strings.TrimSpace(meta["tokens_input_used"])
		if usedText == "" {
			usedText = meta["tokens_used"]
		}
		used, usedOK := parseFloatLoose(usedText)
		avail, availOK := parseFloatLoose(meta["tokens_available"])
		if usedOK && availOK {
			if avail <= 0 {
				return 0, false
			}
			if avail < used {
				total := used + avail
				if total > 0 {
					return (avail / total) * 100, true
				}
			}
			return ((avail - used) / avail) * 100, true
		}
	}
	return 0, false
}

func parseFloatLoose(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	value = strings.ReplaceAll(value, ",", "")
	num, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return num, true
}

func parsePercent(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "%")
	if value == "" {
		return 0, false
	}
	return parseFloatLoose(value)
}
