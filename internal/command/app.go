package command

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"co/internal/agent"
	"co/internal/openai"
	"co/internal/provider"
	"co/internal/transcript"
)

// App is the session, catalog, and history shared by both fronts.
type App struct {
	cat       *provider.Catalog
	home      string
	cwd       string
	noHistory bool

	session *agent.Session
	history *transcript.Writer
	model   string
}

// New starts an app that has no session until a model is activated.
func New(cat *provider.Catalog, home, cwd string, noHistory bool) *App {
	return &App{
		cat:       cat,
		home:      home,
		cwd:       cwd,
		noHistory: noHistory,
	}
}

// Ready reports whether a model has been activated.
func (a *App) Ready() bool {
	return a.session != nil
}

// Model is the selected provider/model-id, or empty.
func (a *App) Model() string {
	return a.model
}

// Session is the conversation. It is nil until the first successful selection.
func (a *App) Session() *agent.Session {
	return a.session
}

// Models lists configured provider/model-id values.
func (a *App) Models(ctx context.Context) ([]string, error) {
	return a.cat.List(ctx)
}

// Providers lists configured providers that accept an API key.
func (a *App) Providers() []string {
	return a.cat.Providers()
}

// Activate selects a model for startup and does not write ~/.co/config.yaml.
func (a *App) Activate(ctx context.Context, raw string) error {
	return a.use(ctx, raw, false)
}

// Select selects a model from /models and records it as the next launch default.
func (a *App) Select(ctx context.Context, raw string) error {
	return a.use(ctx, raw, true)
}

// CanConnect reports whether /connect is valid for this provider.
func (a *App) CanConnect(id string) error {
	return a.cat.CanConnect(id)
}

// Connect stores an API key for a provider that accepts one.
func (a *App) Connect(id, key string) error {
	return a.cat.SaveKey(id, key)
}

// RejectConnect is the usage error for /connect when stdin is not a terminal.
func (a *App) RejectConnect(id string) error {
	if err := a.cat.CanConnect(id); err != nil {
		return err
	}
	p, err := a.cat.Lookup(id)
	if err != nil {
		return err
	}
	if p.APIKeyEnv != "" {
		return fmt.Errorf("connect %s: set %s or run /connect in a terminal", id, p.APIKeyEnv)
	}
	return fmt.Errorf("connect %s: run /connect in a terminal", id)
}

// Resume continues a saved session.
// model overrides the transcript when it is non-empty.
func (a *App) Resume(ctx context.Context, id, model string) error {
	if a.session != nil {
		return fmt.Errorf("session already started")
	}
	loaded, err := transcript.Resume(a.home, a.cwd, id)
	if err != nil {
		return err
	}
	selected := loaded.Model
	if strings.TrimSpace(model) != "" {
		selected = model
	}
	client, wire, ref, err := a.openClient(ctx, selected)
	if err != nil {
		_ = loaded.Writer.Close()
		return err
	}
	var recorder agent.Recorder
	if a.noHistory {
		if err := loaded.Writer.Close(); err != nil {
			slog.Warn("closing session history", "err", err)
		}
	} else {
		a.history = loaded.Writer
		recorder = loaded.Writer
	}
	a.session = agent.NewSession(client, wire, a.cwd, recorder)
	a.session.Restore(loaded.Messages)
	a.model = ref
	if ref != loaded.Model {
		a.noteModel(ref)
	}
	return nil
}

// ExitNote tells the user how to resume, when this process wrote a transcript.
func (a *App) ExitNote() string {
	if a.history == nil || !a.history.Persisted() {
		return ""
	}
	return "resume with: co --resume " + a.history.ID()
}

// Close closes the session transcript.
func (a *App) Close() {
	if a.history == nil {
		return
	}
	if err := a.history.Close(); err != nil {
		slog.Warn("closing session history", "err", err)
	}
	a.history = nil
}

func (a *App) use(ctx context.Context, raw string, persist bool) error {
	client, wire, ref, err := a.openClient(ctx, raw)
	if err != nil {
		return err
	}
	if persist {
		if err := a.cat.SaveSelection(ref); err != nil {
			return err
		}
	}

	if a.session == nil {
		recorder, history := a.openHistory(ref)
		a.history = history
		a.session = agent.NewSession(client, wire, a.cwd, recorder)
		a.model = ref
		return nil
	}

	a.session.SetModel(client, wire)
	if a.model != ref {
		a.noteModel(ref)
	}
	a.model = ref
	return nil
}

func (a *App) openClient(ctx context.Context, raw string) (*openai.Client, string, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", "", err
	}
	ref, err := provider.Parse(raw)
	if err != nil {
		return nil, "", "", err
	}
	ep, err := a.cat.Endpoint(ref)
	if err != nil {
		return nil, "", "", err
	}
	client := openai.NewEndpoint(ep.BaseURL, ep.APIKey, ep.Compat)
	return client, ep.Model, ref.String(), nil
}

func (a *App) openHistory(model string) (agent.Recorder, *transcript.Writer) {
	if a.noHistory {
		return nil, nil
	}
	history, err := transcript.Open(a.home, a.cwd, model)
	if err != nil {
		slog.Warn("session history disabled", "err", err)
		return nil, nil
	}
	return history, history
}

func (a *App) noteModel(model string) {
	if a.history == nil {
		return
	}
	if err := a.history.RecordModel(model); err != nil {
		slog.Warn("session history disabled", "err", err)
		_ = a.history.Close()
		a.history = nil
		a.session.DropRecorder()
	}
}
