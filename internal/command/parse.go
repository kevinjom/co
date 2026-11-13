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
	KindSkill
	KindStats
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
	cmdSkill    = "/skill"
	cmdStats    = "/stats"
)

func slashCommands() []string {
	return []string{cmdModels, cmdConnect, cmdNew, cmdSessions, cmdStats, cmdTheme, cmdSkill}
}

// Parse classifies a line before it can become a chat turn.
// skills are names without the leading slash. /skill lists them.
// A line that names a skill, such as /grilling or /skill grilling, is a chat turn.
// /models and /models <provider>/<model-id> select a model.
// /connect lists providers that accept a key. /connect <provider> skips that list.
// /new starts another session. /sessions lists the open ones.
// /theme lists palettes. /theme <name> selects one.
// /stats prints counts for the current session.
// exit and quit leave the session.
func Parse(line string, skills []string) Parsed {
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
	case cmdStats:
		if arg != "" {
			return Parsed{Kind: KindError, Err: errors.New("usage: /stats")}
		}
		return Parsed{Kind: KindStats}
	case cmdSkill:
		if arg == "" {
			return Parsed{Kind: KindSkill}
		}
		skillName := strings.Fields(arg)[0]
		if _, ok := knownSkill("/"+skillName, skills); !ok {
			return Parsed{Kind: KindError, Err: fmt.Errorf("unknown skill %q", skillName)}
		}
		return Parsed{Kind: KindChat, Arg: line}
	default:
		if _, ok := knownSkill(name, skills); ok {
			return Parsed{Kind: KindChat, Arg: line}
		}
		return Parsed{Kind: KindError, Err: fmt.Errorf("unknown command %q", name)}
	}
}

func knownSkill(command string, skills []string) (string, bool) {
	name := strings.TrimPrefix(command, "/")
	if name == "" || name == command {
		return "", false
	}
	for _, skillName := range skills {
		if skillName == name {
			return skillName, true
		}
	}
	return "", false
}
