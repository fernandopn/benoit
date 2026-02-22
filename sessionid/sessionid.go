package sessionid

import "strings"

const DefaultSessionID = "__default__"

func Normalize(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return DefaultSessionID
	}
	return sessionID
}
