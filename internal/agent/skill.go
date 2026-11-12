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

// skillsOn returns the skills named in one user line, in first-seen order.
func (s *Session) skillsOn(line string) []skill.Skill {
	names := skillsNamed([]string{line}, s.catalog)
	out := make([]skill.Skill, 0, len(names))
	for _, name := range names {
		sk, ok := s.catalog.Get(name)
		if !ok {
			continue
		}
		out = append(out, sk)
	}
	return out
}

func (s *Session) refreshPrompt() {
	if len(s.messages) == 0 || s.messages[0].Role != "system" {
		return
	}
	listed := []skill.Skill{}
	if s.catalog != nil {
		listed = s.catalog.List()
	}
	s.messages[0].Content = systemPrompt(s.cwd, s.lsp, listed)
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
