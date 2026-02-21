package tui

import "github.com/fernandopn/benoit/sessionid"

func resolveTUISessionID(raw string) string {
	resolved, err := sessionid.ResolveInteractive(raw)
	if err != nil {
		fallback, _ := sessionid.ResolveInteractive("")
		return fallback
	}
	return resolved
}
