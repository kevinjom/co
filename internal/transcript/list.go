package transcript

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kevinjom/co/internal/openai"
)

const previewRunes = 60

// Summary is one saved session for a directory.
// Updated is the transcript file's modification time.
// Model is the latest model, after any model-change records.
// Preview is the first user message, collapsed onto one line.
type Summary struct {
	ID      string
	Updated time.Time
	Model   string
	Preview string
}

// List returns up to limit saved sessions for cwd, newest first.
// Newest is the most recently modified file, the same session resume latest opens.
// A missing sessions directory is an empty list.
func List(home, cwd string, limit int) ([]Summary, error) {
	if strings.TrimSpace(home) == "" {
		return nil, fmt.Errorf("session history: home directory is empty")
	}
	if limit < 1 {
		return nil, fmt.Errorf("session history: limit must be at least 1")
	}
	resolved, err := resolveCwd(cwd)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".co", "projects", projectName(resolved), "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Summary{}, nil
		}
		return nil, fmt.Errorf("session history: %w", err)
	}

	type candidate struct {
		id   string
		path string
		mod  time.Time
	}
	candidates := []candidate{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(name, ".jsonl")
		if !validSessionID(id) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("session history: %w", err)
		}
		candidates = append(candidates, candidate{
			id:   id,
			path: filepath.Join(dir, name),
			mod:  info.ModTime(),
		})
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		if a.mod.Equal(b.mod) {
			return strings.Compare(b.id, a.id)
		}
		if a.mod.After(b.mod) {
			return -1
		}
		return 1
	})

	summaries := []Summary{}
	for _, item := range candidates {
		if len(summaries) == limit {
			break
		}
		summary, err := readSummary(
			item.path,
			item.id,
			resolved,
			item.mod,
		)
		if err != nil {
			var input *InputError
			if errors.As(err, &input) {
				slog.Warn("skipping session", "id", item.id, "err", err)
				continue
			}
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func readSummary(path, id, cwd string, updated time.Time) (Summary, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Summary{}, fmt.Errorf("session history: %w", err)
	}
	parsed, err := parseSession(id, cwd, raw)
	if err != nil {
		return Summary{}, err
	}
	return Summary{
		ID:      id,
		Updated: updated,
		Model:   parsed.model,
		Preview: oneLine(firstUserText(parsed.messages)),
	}, nil
}

func firstUserText(messages []openai.Message) string {
	for _, msg := range messages {
		if msg.Role == "user" {
			return msg.Content
		}
	}
	return ""
}

func oneLine(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= previewRunes {
		return text
	}
	return string(runes[:previewRunes-len("...")]) + "..."
}
