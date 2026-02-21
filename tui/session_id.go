package tui

import "github.com/google/uuid"

func newTUISessionID() string {
	return uuid.NewString()
}
