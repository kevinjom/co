package agent

import (
	"strings"

	"github.com/kevinjom/co/internal/skill"
)

// SetCatalog makes these skills available to the session.
// A nil catalog clears them.
func (s *Session) SetCatalog(catalog *skill.Catalog) {
	if s == nil {
		return
	}
	s.catalog = catalog
	s.refreshPrompt()
}

// applyTurnSkills adds the skills named in lines to the system prompt.
// The names last only until clearTurnSkills.
func (s *Session) applyTurnSkills(lines []string) {
	if s == nil {
		return
	}
	s.active = skillsNamed(lines, s.catalog)
	s.refreshPrompt()
}

// clearTurnSkills drops the skills applied for the turn that just finished.
func (s *Session) clearTurnSkills() {
	if s == nil || len(s.active) == 0 {
		return
	}
	s.active = []string{}
	s.refreshPrompt()
}

func (s *Session) refreshPrompt() {
	if len(s.messages) == 0 || s.messages[0].Role != "system" {
		return
	}
	listed := []skill.Skill{}
	if s.catalog != nil {
		listed = s.catalog.List()
	}
	active := make([]skill.Skill, 0, len(s.active))
	for _, name := range s.active {
		if s.catalog == nil {
			continue
		}
		sk, ok := s.catalog.Get(name)
		if !ok {
			continue
		}
		active = append(active, sk)
	}
	s.messages[0].Content = systemPrompt(
		s.cwd,
		s.lsp,
		listed,
		active,
	)
}

// skillsNamed returns each skill mentioned as /name, in first-seen order.
// /skill name is the same mention as /name.
func skillsNamed(lines []string, catalog *skill.Catalog) []string {
	if catalog == nil {
		return []string{}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, line := range lines {
		fields := strings.Fields(line)
		for i := 0; i < len(fields); i++ {
			name, ok := skillToken(fields[i])
			if !ok {
				continue
			}
			if name == "skill" && i+1 < len(fields) {
				if _, found := catalog.Get(fields[i+1]); found {
					name = fields[i+1]
					i++
				}
			}
			if _, found := catalog.Get(name); !found || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func skillToken(field string) (string, bool) {
	name := strings.TrimPrefix(field, "/")
	if name == "" || name == field || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}
