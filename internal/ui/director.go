package ui

import (
	"context"

	"co/internal/agent"
)

// Director is the session and catalog the TUI drives.
// Select persists the choice. Models may return ids together with an error
// when a live listing fails and other providers still have models.
type Director interface {
	Models(context.Context) ([]string, error)
	Select(context.Context, string) error
	CanConnect(string) error
	Connect(string, string) error
	Model() string
	Session() *agent.Session
	Ready() bool
}
