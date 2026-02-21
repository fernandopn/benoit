package tui

import "github.com/fernandopn/benoit/session"

func resolveTUISessionID(raw string) string {
	resolved, err := session.ResolveInteractiveSessionID(raw)
	if err != nil {
		fallback, _ := session.ResolveInteractiveSessionID("")
		return fallback
	}
	return resolved
}
