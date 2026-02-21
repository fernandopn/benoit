package sessionid

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

const (
	Default       = "__default__"
	maxSessionLen = 256
)

var errSessionIDHasControlChars = errors.New("session ID cannot contain control characters")

func Normalize(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Default
	}
	return sessionID
}

func Validate(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	if len(sessionID) > maxSessionLen {
		return fmt.Errorf("session ID cannot exceed %d characters", maxSessionLen)
	}
	for _, r := range sessionID {
		if unicode.IsControl(r) {
			return errSessionIDHasControlChars
		}
	}
	return nil
}

func ResolveInteractive(sessionID string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	if err := Validate(sessionID); err != nil {
		return "", err
	}
	return sessionID, nil
}

func Telegram(userID int64) string {
	if userID == 0 {
		return ""
	}
	return "telegram:" + strconv.FormatInt(userID, 10)
}
