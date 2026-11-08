// Package transcript stores one session in a JSONL file.
// Messages are appended when they are complete. Streaming deltas are not stored.
package transcript

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kevinjom/co/internal/llm"
)

const version = 1

// Writer appends a session header and completed messages.
// The file is created on the first Record, so an unused writer leaves nothing behind.
type Writer struct {
	path    string
	id      string
	cwd     string
	model   string
	started time.Time
	file    *os.File
	seq     int
	closed  bool
	mu      sync.Mutex
}

type sessionHeader struct {
	Type    string `json:"type"`
	V       int    `json:"v"`
	ID      string `json:"id"`
	Cwd     string `json:"cwd"`
	Model   string `json:"model"`
	Started string `json:"started"`
}

type messageRecord struct {
	Type        string      `json:"type"`
	V           int         `json:"v"`
	Seq         int         `json:"seq"`
	TS          string      `json:"ts"`
	Interrupted bool        `json:"interrupted"`
	Message     llm.Message `json:"message"`
}

type modelRecord struct {
	Type  string `json:"type"`
	V     int    `json:"v"`
	Seq   int    `json:"seq"`
	TS    string `json:"ts"`
	Model string `json:"model"`
}

type titleRecord struct {
	Type  string `json:"type"`
	V     int    `json:"v"`
	Seq   int    `json:"seq"`
	TS    string `json:"ts"`
	Title string `json:"title"`
}

type skillRecord struct {
	Type string `json:"type"`
	V    int    `json:"v"`
	Seq  int    `json:"seq"`
	TS   string `json:"ts"`
	Name string `json:"name"`
}

// Open prepares a session file under home/.co/projects/<project>/sessions.
// home is the user home directory. cwd is the project directory.
func Open(home, cwd, model string) (*Writer, error) {
	if strings.TrimSpace(home) == "" {
		return nil, fmt.Errorf("session history: home directory is empty")
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("session history: model is empty")
	}
	resolved, err := resolveCwd(cwd)
	if err != nil {
		return nil, err
	}
	id, err := newSessionID(time.Now().UTC())
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".co", "projects", projectName(resolved), "sessions")
	return &Writer{
		path:    filepath.Join(dir, id+".jsonl"),
		id:      id,
		cwd:     resolved,
		model:   model,
		started: time.Now().UTC(),
	}, nil
}

func resolveCwd(cwd string) (string, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("session history: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("session history: %w", err)
	}
	abs, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("session history: %w", err)
	}
	return abs, nil
}

// projectName is the absolute path with every separator replaced by "-".
func projectName(abs string) string {
	return strings.ReplaceAll(abs, string(os.PathSeparator), "-")
}

// ID is the session id, also the file name without .jsonl.
func (w *Writer) ID() string {
	if w == nil {
		return ""
	}
	return w.id
}

// Persisted reports that the transcript file exists.
// A writer that has not recorded anything yet is not persisted.
func (w *Writer) Persisted() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return true
	}
	info, err := os.Stat(w.path)
	return err == nil && !info.IsDir()
}

func newSessionID(now time.Time) (string, error) {
	var buf [2]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("session history: %w", err)
	}
	return now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(buf[:]), nil
}

// Record appends one completed message.
// interrupted marks an assistant message cut short by cancellation.
func (w *Writer) Record(msg llm.Message, interrupted bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writeRecord(func(seq int) any {
		return messageRecord{
			Type:        "message",
			V:           version,
			Seq:         seq,
			TS:          time.Now().UTC().Format(time.RFC3339Nano),
			Interrupted: interrupted,
			Message:     msg,
		}
	})
}

// RecordModel appends one model-change line.
// The session header keeps the model from Open and is not rewritten.
func (w *Writer) RecordModel(model string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writeRecord(func(seq int) any {
		return modelRecord{
			Type:  "model",
			V:     version,
			Seq:   seq,
			TS:    time.Now().UTC().Format(time.RFC3339Nano),
			Model: model,
		}
	})
}

// RecordSkill appends the name of a skill activated in this session.
// The file is created on this write when nothing has been stored yet.
func (w *Writer) RecordSkill(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("session history: skill name is empty")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writeRecord(func(seq int) any {
		return skillRecord{
			Type: "skill",
			V:    version,
			Seq:  seq,
			TS:   time.Now().UTC().Format(time.RFC3339Nano),
			Name: name,
		}
	})
}

// RecordTitle appends the generated name of this session.
// The header is not rewritten. An empty title writes nothing.
func (w *Writer) RecordTitle(title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writeRecord(func(seq int) any {
		return titleRecord{
			Type:  "title",
			V:     version,
			Seq:   seq,
			TS:    time.Now().UTC().Format(time.RFC3339Nano),
			Title: title,
		}
	})
}

// writeRecord assumes w.mu is held.
func (w *Writer) writeRecord(build func(seq int) any) error {
	if w.closed {
		return fmt.Errorf("session history: closed")
	}
	if err := w.ensure(); err != nil {
		return err
	}
	w.seq++
	return w.writeLine(build(w.seq))
}

func (w *Writer) ensure() error {
	if w.file != nil {
		return nil
	}
	dir := filepath.Dir(w.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	w.file = file
	header := sessionHeader{
		Type:    "session",
		V:       version,
		ID:      w.id,
		Cwd:     w.cwd,
		Model:   w.model,
		Started: w.started.Format(time.RFC3339),
	}
	if err := w.writeLine(header); err != nil {
		w.file.Close()
		os.Remove(w.path)
		w.file = nil
		return err
	}
	return nil
}

// writeLine is one Write, so a crash can tear at most the last line.
func (w *Writer) writeLine(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := w.file.Write(data); err != nil {
		return err
	}
	return nil
}

// Close closes the file. It is safe before any Record.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	w.closed = true
	err := w.file.Close()
	w.file = nil
	return err
}
