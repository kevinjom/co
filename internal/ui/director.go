package ui

import (
	"context"

	"github.com/kevinjom/co/internal/agent"
	"github.com/kevinjom/co/internal/skill"
)

// Director is the session and catalog the TUI drives.
// Select persists the choice and retargets the given session.
// A nil session creates the first one. Open starts another session.
// Models lists configured provider/model-id values.
// Providers lists providers that accept an API key.
// SaveTheme stores the palette name. An isolated config file is left alone.
// Skills lists the skills found at startup.
type Director interface {
	Models(context.Context) ([]string, error)
	Select(context.Context, string, *agent.Session) error
	Open(context.Context) (*agent.Session, error)
	Providers() []string
	CanConnect(string) error
	Connect(string, string) error
	SaveTheme(string) error
	Skills() []skill.Skill
	Model() string
	Session() *agent.Session
	Ready() bool
	ExitNote() string
}
