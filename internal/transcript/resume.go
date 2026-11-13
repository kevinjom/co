package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kevinjom/co/internal/llm"
)

// InputError is a session id or transcript the user must correct.
type InputError struct {
	msg string
}

func (e *InputError) Error() string {
	if e == nil {
		return ""
	}
	return e.msg
}

func inputf(format string, args ...any) error {
	return &InputError{msg: fmt.Sprintf(format, args...)}
}

// SavedSkill is a skill named by one stored user message.
// Body is the SKILL.md text from the turn that named it.
type SavedSkill struct {
	Name string
	Dir  string
	Body string
}

// Loaded is an existing session opened for appending.
// Model is the latest model, after any model-change records.
// Messages are the completed turns, without the system prompt.
// Skills are names from older skill records that are not tied to a message.
// MessageSkills is parallel to Messages. A message that named no skill has an empty slice.
type Loaded struct {
	Writer        *Writer
	Model         string
	Title         string
	Messages      []llm.Message
	Skills        []string
	MessageSkills [][]SavedSkill
}

// Resume opens home/.co/projects/<project>/sessions/<id>.jsonl for appending.
// id may be "latest", which is the most recently modified session in this project.
// A trailing partial line, from a crash during a write, is discarded.
func Resume(home, cwd, id string) (*Loaded, error) {
	if strings.TrimSpace(home) == "" {
		return nil, fmt.Errorf("session history: home directory is empty")
	}
	id = strings.TrimSpace(id)
	id = strings.TrimSuffix(id, ".jsonl")
	if id == "" {
		return nil, inputf("session id is required")
	}
	resolved, err := resolveCwd(cwd)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".co", "projects", projectName(resolved), "sessions")
	if id == "latest" {
		id, err = latestID(dir)
		if err != nil {
			return nil, err
		}
	}
	if !validSessionID(id) {
		return nil, inputf("invalid session id %q", id)
	}

	path := filepath.Join(dir, id+".jsonl")
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, inputf("session %q not found", id)
		}
		return nil, fmt.Errorf("session history: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, fmt.Errorf("session history: %w", err)
	}
	raw, err := io.ReadAll(file)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("session history: %w", err)
	}

	parsed, err := parseSession(id, resolved, raw)
	if err != nil {
		file.Close()
		return nil, err
	}
	if parsed.kept < len(raw) {
		if err := file.Truncate(int64(parsed.kept)); err != nil {
			file.Close()
			return nil, fmt.Errorf("session history: %w", err)
		}
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		file.Close()
		return nil, fmt.Errorf("session history: %w", err)
	}

	title := parsed.title
	if title == "" {
		title = FallbackTitle(firstUserText(parsed.messages))
	}
	return &Loaded{
		Writer: &Writer{
			path:  path,
			id:    id,
			cwd:   resolved,
			model: parsed.headerModel,
			file:  file,
			seq:   parsed.seq,
		},
		Model:         parsed.model,
		Title:         title,
		Messages:      parsed.messages,
		Skills:        parsed.skills,
		MessageSkills: parsed.messageSkills,
	}, nil
}

type parsedSession struct {
	headerModel   string
	model         string
	title         string
	messages      []llm.Message
	skills        []string
	messageSkills [][]SavedSkill
	msgIndex      map[int]int
	seq           int
	kept          int
}

func parseSession(id, cwd string, raw []byte) (parsedSession, error) {
	kept := raw
	if len(kept) > 0 && kept[len(kept)-1] != '\n' {
		idx := bytes.LastIndexByte(kept, '\n')
		if idx < 0 {
			return parsedSession{}, inputf("session %q is incomplete", id)
		}
		kept = kept[:idx+1]
	}
	if len(kept) == 0 {
		return parsedSession{}, inputf("session %q is empty", id)
	}

	lines := bytes.Split(bytes.TrimSuffix(kept, []byte("\n")), []byte("\n"))
	var header sessionHeader
	if err := json.Unmarshal(lines[0], &header); err != nil {
		return parsedSession{}, inputf("session %q is not a co transcript", id)
	}
	if header.Type != "session" || header.V != version {
		return parsedSession{}, inputf("session %q is not a co transcript", id)
	}
	if header.Cwd != cwd {
		return parsedSession{}, inputf("session %q belongs to %s", id, header.Cwd)
	}
	if strings.TrimSpace(header.Model) == "" {
		return parsedSession{}, inputf("session %q has no model", id)
	}

	parsed := parsedSession{
		headerModel:   header.Model,
		model:         header.Model,
		messages:      []llm.Message{},
		skills:        []string{},
		messageSkills: [][]SavedSkill{},
		msgIndex:      map[int]int{},
		kept:          len(kept),
	}
	for i, line := range lines[1:] {
		next, err := applyRecord(line, &parsed)
		if err != nil {
			return parsedSession{}, inputf("session %q: line %d: %s", id, i+2, err)
		}
		parsed = next
		parsed.kept = len(kept)
	}
	return parsed, nil
}

func applyRecord(line []byte, parsed *parsedSession) (parsedSession, error) {
	var kind struct {
		Type string `json:"type"`
		V    int    `json:"v"`
		Seq  int    `json:"seq"`
	}
	if err := json.Unmarshal(line, &kind); err != nil {
		return parsedSession{}, err
	}
	if kind.V != version {
		return parsedSession{}, fmt.Errorf("unsupported version %d", kind.V)
	}
	next := *parsed
	if kind.Seq > next.seq {
		next.seq = kind.Seq
	}
	switch kind.Type {
	case "message":
		var rec messageRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return parsedSession{}, err
		}
		if strings.TrimSpace(rec.Message.Role) == "" {
			return parsedSession{}, fmt.Errorf("message has no role")
		}
		if rec.Usage != nil {
			rec.Message.Usage = &llm.Usage{Input: rec.Usage.Input, Output: rec.Usage.Output}
		}
		next.msgIndex[kind.Seq] = len(next.messages)
		next.messages = append(next.messages, rec.Message)
		next.messageSkills = append(next.messageSkills, []SavedSkill{})
		return next, nil
	case "model":
		var rec modelRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return parsedSession{}, err
		}
		if strings.TrimSpace(rec.Model) == "" {
			return parsedSession{}, fmt.Errorf("model is empty")
		}
		next.model = rec.Model
		return next, nil
	case "title":
		var rec titleRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return parsedSession{}, err
		}
		if strings.TrimSpace(rec.Title) == "" {
			return parsedSession{}, fmt.Errorf("title is empty")
		}
		next.title = rec.Title
		return next, nil
	case "skill":
		var rec skillRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return parsedSession{}, err
		}
		if strings.TrimSpace(rec.Name) == "" {
			return parsedSession{}, fmt.Errorf("skill name is empty")
		}
		if rec.At == 0 {
			if rec.Body != "" || rec.Dir != "" {
				return parsedSession{}, fmt.Errorf("skill %q is not attached to a message", rec.Name)
			}
			next.skills = append(next.skills, rec.Name)
			return next, nil
		}
		idx, ok := next.msgIndex[rec.At]
		if !ok {
			return parsedSession{}, fmt.Errorf("skill %q is not attached to a message", rec.Name)
		}
		if strings.TrimSpace(rec.Body) == "" {
			return parsedSession{}, fmt.Errorf("skill body is empty")
		}
		next.messageSkills[idx] = append(next.messageSkills[idx], SavedSkill{
			Name: rec.Name,
			Dir:  rec.Dir,
			Body: rec.Body,
		})
		return next, nil
	default:
		return parsedSession{}, fmt.Errorf("unknown record %q", kind.Type)
	}
}

func latestID(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", inputf("no session to resume")
		}
		return "", fmt.Errorf("session history: %w", err)
	}
	var best string
	var bestTime time.Time
	found := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", fmt.Errorf("session history: %w", err)
		}
		id := strings.TrimSuffix(name, ".jsonl")
		if !validSessionID(id) {
			continue
		}
		if !found || info.ModTime().After(bestTime) || (info.ModTime().Equal(bestTime) && id > best) {
			found = true
			best = id
			bestTime = info.ModTime()
		}
	}
	if !found {
		return "", inputf("no session to resume")
	}
	return best, nil
}

func validSessionID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}
