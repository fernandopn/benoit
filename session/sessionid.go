package session

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

const (
	DefaultSessionID = "__default__"
	maxSessionIDLen  = 256
)

var errSessionIDHasControlChars = errors.New("session ID cannot contain control characters")

func NormalizeSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return DefaultSessionID
	}
	return sessionID
}

func ValidateSessionID(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	if len(sessionID) > maxSessionIDLen {
		return fmt.Errorf("session ID cannot exceed %d characters", maxSessionIDLen)
	}
	for _, r := range sessionID {
		if unicode.IsControl(r) {
			return errSessionIDHasControlChars
		}
	}
	return nil
}

func ResolveInteractiveSessionID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = uuid.NewString()
	}
	if err := ValidateSessionID(raw); err != nil {
		return "", err
	}
	return raw, nil
}

func TelegramSessionID(userID int64) string {
	if userID == 0 {
		return ""
	}
	return "telegram:" + strconv.FormatInt(userID, 10)
}
