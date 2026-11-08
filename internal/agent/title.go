package agent

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/kevinjom/co/internal/llm"
	"github.com/kevinjom/co/internal/transcript"
)

const (
	titleSystem = "You name a chat from the user's first message. " +
		"Reply with a short title, at most six words. " +
		"No quotes. No trailing punctuation. The title only."
	titleWords   = 6
	titleTimeout = 2 * time.Minute
	titleRunes   = 2000
)

// titler stores the generated session name.
type titler interface {
	RecordTitle(title string) error
}

// Wait waits for the title started by the first message.
// Close the transcript after Wait so that name is on disk.
func (s *Session) Wait() {
	if s == nil {
		return
	}
	s.titleWG.Wait()
}

// Title is the latest session name. It is empty until the first prompt is named.
func (s *Session) Title() string {
	if s == nil {
		return ""
	}
	s.titleMu.Lock()
	defer s.titleMu.Unlock()
	return s.title
}

// SetTitle stores a name, such as one loaded from a transcript.
func (s *Session) SetTitle(title string) {
	s.setTitle(title)
}

// NotifyTitle registers the function called after the name changes.
// The function runs outside the session lock and must not block.
func (s *Session) NotifyTitle(fn func(string)) {
	if s == nil {
		return
	}
	s.titleMu.Lock()
	s.onTitle = fn
	s.titleMu.Unlock()
}

func (s *Session) setTitle(title string) {
	title = strings.TrimSpace(title)
	if s == nil || title == "" {
		return
	}
	s.titleMu.Lock()
	if s.title == title {
		s.titleMu.Unlock()
		return
	}
	s.title = title
	fn := s.onTitle
	s.titleMu.Unlock()
	if fn != nil {
		fn(title)
	}
}

// storeFallbackTitle writes the first line of the prompt, at most ten words.
// The model name replaces it when the background request returns. A naming failure leaves this one.
func (s *Session) storeFallbackTitle(userText string) {
	title := transcript.FallbackTitle(userText)
	if title == "" {
		return
	}
	s.setTitle(title)
	namer, ok := s.recorder.(titler)
	if !ok {
		return
	}
	if err := namer.RecordTitle(title); err != nil {
		slog.Warn("chat title", "err", err)
	}
}

// refineTitle asks the model for a short name from the first prompt.
// The request runs beside the reply and uses only that prompt.
func (s *Session) refineTitle(userText string) {
	namer, ok := s.recorder.(titler)
	if !ok || strings.TrimSpace(userText) == "" {
		return
	}
	client := s.client
	model := s.model
	fallback := transcript.FallbackTitle(userText)
	s.titleWG.Add(1)
	go func() {
		defer s.titleWG.Done()
		title, err := chatTitle(client, model, userText)
		if err != nil {
			slog.Warn("chat title", "err", err)
			return
		}
		if title == "" || title == fallback {
			return
		}
		s.setTitle(title)
		if err := namer.RecordTitle(title); err != nil {
			slog.Warn("chat title", "err", err)
		}
	}()
}

func chatTitle(client llm.Chatter, model, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), titleTimeout)
	defer cancel()
	msg, err := client.ChatStream(ctx, model, []llm.Message{
		{Role: "system", Content: titleSystem},
		{Role: "user", Content: clipRunes(prompt, titleRunes)},
	}, nil, nil)
	if err != nil {
		return "", err
	}
	return cleanTitle(msg.Content), nil
}

func cleanTitle(raw string) string {
	line := ""
	for _, part := range strings.Split(raw, "\n") {
		part = strings.TrimSpace(part)
		if part != "" {
			line = part
			break
		}
	}
	line = strings.Trim(line, `"'`)
	line = strings.TrimSpace(line)
	line = strings.TrimRight(line, ".,;:!?")
	line = strings.TrimSpace(line)
	words := strings.Fields(line)
	if len(words) > titleWords {
		words = words[:titleWords]
	}
	return strings.Join(words, " ")
}

func clipRunes(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n])
}
