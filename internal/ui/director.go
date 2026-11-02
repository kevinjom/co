package ui

import (
	"context"

	"github.com/kevinjom/co/internal/agent"
)

// Director is the session and catalog the TUI drives.
// Select persists the choice. Models lists configured provider/model-id values.
// Providers lists providers that accept an API key.
type Director interface {
	Models(context.Context) ([]string, error)
	Select(context.Context, string) error
	Providers() []string
	CanConnect(string) error
	Connect(string, string) error
	Model() string
	Session() *agent.Session
	Ready() bool
	ExitNote() string
}
