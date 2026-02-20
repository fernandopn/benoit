package utils

import (
	"strconv"
	"strings"
)

func ContextLeftPercent(value string, meta map[string]string) (float64, bool) {
	if percentUsed, ok := parsePercent(value); ok {
		return 100 - percentUsed, true
	}
	if meta != nil {
		used, usedOK := parseFloatLoose(meta["tokens_used"])
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
