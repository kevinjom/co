package command

import (
	"sort"
	"strings"
)

// Group is one heading in the slash list.
// Matches are the commands under that heading, in display order.
type Group struct {
	Heading string
	Matches []string
}

// Completion is one tab step for a slash command name.
// Groups split the matches into commands and skills.
// Matches are the selectable names, without headings.
// Value is the name tab inserts.
// Value is empty when the prefix matches nothing, or when it is already the only match.
type Completion struct {
	Groups  []Group
	Matches []string
	Value   string
	Index   int
}

// Complete finishes a slash command name.
// stem is the text before an argument. index selects a match when several
// commands share that prefix, and it wraps.
// skills are names without the leading slash.
func Complete(stem string, index int, skills []string) Completion {
	if !strings.HasPrefix(stem, "/") || strings.ContainsAny(stem, " \t\n") {
		return Completion{}
	}
	commands := []string{}
	for _, name := range slashCommands() {
		if strings.HasPrefix(name, stem) {
			commands = append(commands, name)
		}
	}
	skillCommands := skillMatches(stem, skills)
	groups := []Group{}
	matches := []string{}
	if len(commands) > 0 {
		groups = append(groups, Group{Heading: "commands", Matches: commands})
		matches = append(matches, commands...)
	}
	if len(skillCommands) > 0 {
		groups = append(groups, Group{Heading: "skills", Matches: skillCommands})
		matches = append(matches, skillCommands...)
	}
	if len(matches) == 0 {
		return Completion{}
	}
	if len(matches) == 1 && matches[0] == stem {
		return Completion{Groups: groups, Matches: matches}
	}
	index %= len(matches)
	if index < 0 {
		index += len(matches)
	}
	return Completion{
		Groups:  groups,
		Matches: matches,
		Value:   matches[index],
		Index:   index,
	}
}

// IsSkill reports that name is one of the skill commands in this completion.
func (c Completion) IsSkill(name string) bool {
	for _, group := range c.Groups {
		if group.Heading != "skills" {
			continue
		}
		for _, match := range group.Matches {
			if match == name {
				return true
			}
		}
	}
	return false
}

func skillMatches(stem string, skills []string) []string {
	names := append([]string{}, skills...)
	sort.Strings(names)
	seen := map[string]bool{}
	out := []string{}
	for _, name := range names {
		command := "/" + name
		if seen[command] || !strings.HasPrefix(command, stem) {
			continue
		}
		seen[command] = true
		out = append(out, command)
	}
	return out
}
