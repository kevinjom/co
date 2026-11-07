// Package skill loads Agent Skills from the working directory and the home directory.
package skill

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	maxNameLen        = 64
	maxDescriptionLen = 1024
)

// Skill is one valid SKILL.md.
// Dir is the project-relative directory, such as .agents/skills/grilling.
// It is empty for a skill that lives under the home directory.
// Body is the full file, including the frontmatter.
type Skill struct {
	Name        string
	Description string
	Dir         string
	Body        string
}

// Catalog is the skills found at startup.
type Catalog struct {
	byName map[string]Skill
	order  []string
}

type frontmatter struct {
	Name          string `yaml:"name"`
	Description   string `yaml:"description"`
	License       string `yaml:"license"`
	Compatibility string `yaml:"compatibility"`
	Metadata      any    `yaml:"metadata"`
	AllowedTools  string `yaml:"allowed-tools"`
}

// reserved names are built-in slash commands, without the leading slash.
var reserved = map[string]bool{
	"models":   true,
	"connect":  true,
	"new":      true,
	"sessions": true,
	"theme":    true,
	"skill":    true,
}

// Load reads skills from <cwd>/.agents/skills and <home>/.co/skills.
// A missing root is empty. The same name in the project replaces the home skill.
// A directory that breaks the spec is skipped and logged.
func Load(cwd, home string) *Catalog {
	cat := &Catalog{
		byName: map[string]Skill{},
		order:  []string{},
	}
	cat.scan(filepath.Join(home, ".co", "skills"), false)
	cat.scan(filepath.Join(cwd, ".agents", "skills"), true)
	sort.Strings(cat.order)
	return cat
}

// List returns the skills in name order.
func (c *Catalog) List() []Skill {
	if c == nil {
		return []Skill{}
	}
	out := make([]Skill, 0, len(c.order))
	for _, name := range c.order {
		out = append(out, c.byName[name])
	}
	return out
}

// Get returns the skill with that name.
func (c *Catalog) Get(name string) (Skill, bool) {
	if c == nil {
		return Skill{}, false
	}
	sk, ok := c.byName[name]
	return sk, ok
}

// Names returns the skill names in name order.
func (c *Catalog) Names() []string {
	if c == nil {
		return []string{}
	}
	out := make([]string, len(c.order))
	copy(out, c.order)
	return out
}

func (c *Catalog) scan(root string, project bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("skills", "path", root, "err", err)
		}
		return
	}
	for _, entry := range entries {
		dirName := entry.Name()
		path := filepath.Join(root, dirName)
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			slog.Warn("skill", "name", dirName, "err", err)
			continue
		}
		sk, err := parseSkill(dirName, string(raw), project)
		if err != nil {
			slog.Warn("skill", "name", dirName, "err", err)
			continue
		}
		c.add(sk)
	}
}

func (c *Catalog) add(sk Skill) {
	if _, ok := c.byName[sk.Name]; !ok {
		c.order = append(c.order, sk.Name)
	}
	c.byName[sk.Name] = sk
}

func parseSkill(dirName, raw string, project bool) (Skill, error) {
	text, ok := frontmatterYAML(raw)
	if !ok {
		return Skill{}, fmt.Errorf("missing frontmatter")
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(text), &fm); err != nil {
		return Skill{}, err
	}
	if err := validName(fm.Name, dirName); err != nil {
		return Skill{}, err
	}
	if strings.TrimSpace(fm.Description) == "" {
		return Skill{}, fmt.Errorf("description is empty")
	}
	if utf8.RuneCountInString(fm.Description) > maxDescriptionLen {
		return Skill{}, fmt.Errorf("description is longer than %d characters", maxDescriptionLen)
	}
	if reserved[fm.Name] {
		return Skill{}, fmt.Errorf("name %q shadows a command", fm.Name)
	}
	dir := ""
	if project {
		dir = ".agents/skills/" + fm.Name
	}
	return Skill{
		Name:        fm.Name,
		Description: fm.Description,
		Dir:         dir,
		Body:        raw,
	}, nil
}

func validName(name, dirName string) error {
	if name != dirName {
		return fmt.Errorf("name %q does not match directory %q", name, dirName)
	}
	count := utf8.RuneCountInString(name)
	if count < 1 || count > maxNameLen {
		return fmt.Errorf("name %q must be 1 to %d characters", name, maxNameLen)
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		return fmt.Errorf("name %q has a bad hyphen", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return fmt.Errorf("name %q has a character outside a-z, 0-9, and hyphen", name)
		}
	}
	return nil
}

// frontmatterYAML returns the YAML between the opening and closing --- lines.
func frontmatterYAML(raw string) (string, bool) {
	raw = strings.TrimPrefix(raw, "\uFEFF")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(raw, "---\n") {
		return "", false
	}
	lines := strings.Split(raw, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			return strings.Join(lines[1:i], "\n"), true
		}
	}
	return "", false
}
