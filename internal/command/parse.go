package command

import (
	"errors"
	"fmt"
	"strings"
)

// Kind is what a submitted line asked for.
type Kind int

const (
	KindChat Kind = iota
	KindQuit
	KindModels
	KindConnect
	KindError
)

// Parsed is one submitted line.
type Parsed struct {
	Kind Kind
	Arg  string
	Err  error
}

const (
	cmdModels  = "/models"
	cmdConnect = "/connect"
)

func slashCommands() []string {
	return []string{cmdModels, cmdConnect}
}

// Parse classifies a line before it can become a chat turn.
// /models and /models <provider>/<model-id> select a model.
// /connect <provider> stores a key. exit and quit leave the session.
func Parse(line string) Parsed {
	line = strings.TrimSpace(line)
	switch line {
	case "exit", "quit":
		return Parsed{Kind: KindQuit}
	}
	if !strings.HasPrefix(line, "/") {
		return Parsed{Kind: KindChat, Arg: line}
	}

	name, arg, _ := strings.Cut(line, " ")
	arg = strings.TrimSpace(arg)
	switch name {
	case cmdModels:
		return Parsed{Kind: KindModels, Arg: arg}
	case cmdConnect:
		if arg == "" || strings.ContainsAny(arg, " \t") {
			return Parsed{Kind: KindError, Err: errors.New("usage: /connect <provider>")}
		}
		return Parsed{Kind: KindConnect, Arg: arg}
	default:
		return Parsed{Kind: KindError, Err: fmt.Errorf("unknown command %q", name)}
	}
}
