package bubbletea

import (
	"strconv"
	"strings"
)

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
