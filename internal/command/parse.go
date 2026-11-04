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
	KindNew
	KindSessions
	KindTheme
	KindError
)

// Parsed is one submitted line.
type Parsed struct {
	Kind Kind
	Arg  string
	Err  error
}

const (
	cmdModels   = "/models"
	cmdConnect  = "/connect"
	cmdNew      = "/new"
	cmdSessions = "/sessions"
	cmdTheme    = "/theme"
)

func slashCommands() []string {
	return []string{cmdModels, cmdConnect, cmdNew, cmdSessions, cmdTheme}
}

// Parse classifies a line before it can become a chat turn.
// /models and /models <provider>/<model-id> select a model.
// /connect lists providers that accept a key. /connect <provider> skips that list.
// /new starts another session. /sessions lists the open ones.
// /theme lists palettes. /theme <name> selects one.
// exit and quit leave the session.
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
		if strings.ContainsAny(arg, " \t") {
			return Parsed{Kind: KindError, Err: errors.New("usage: /connect [provider]")}
		}
		return Parsed{Kind: KindConnect, Arg: arg}
	case cmdNew:
		if arg != "" {
			return Parsed{Kind: KindError, Err: errors.New("usage: /new")}
		}
		return Parsed{Kind: KindNew}
	case cmdSessions:
		if arg != "" {
			return Parsed{Kind: KindError, Err: errors.New("usage: /sessions")}
		}
		return Parsed{Kind: KindSessions}
	case cmdTheme:
		if strings.ContainsAny(arg, " \t") {
			return Parsed{Kind: KindError, Err: errors.New("usage: /theme [name]")}
		}
		return Parsed{Kind: KindTheme, Arg: arg}
	default:
		return Parsed{Kind: KindError, Err: fmt.Errorf("unknown command %q", name)}
	}
}
