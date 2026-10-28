package command

import "strings"

// Completion is one tab step for a slash command name.
// Matches are the commands that share the typed prefix. Value is the name tab inserts.
// Value is empty when the prefix matches nothing, or when it is already the only match.
type Completion struct {
	Matches []string
	Value   string
	Index   int
}

// Complete finishes a slash command name.
// stem is the text before an argument. index selects a match when several
// commands share that prefix, and it wraps.
func Complete(stem string, index int) Completion {
	matches := candidates(stem)
	if len(matches) == 0 {
		return Completion{}
	}
	if len(matches) == 1 && matches[0] == stem {
		return Completion{Matches: matches}
	}
	index %= len(matches)
	if index < 0 {
		index += len(matches)
	}
	return Completion{
		Matches: matches,
		Value:   matches[index],
		Index:   index,
	}
}

func candidates(stem string) []string {
	if !strings.HasPrefix(stem, "/") || strings.ContainsAny(stem, " \t\n") {
		return nil
	}
	matches := make([]string, 0, len(slashCommands()))
	for _, name := range slashCommands() {
		if strings.HasPrefix(name, stem) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	return matches
}
