package command

import (
	"context"
	"fmt"
	"log/slog"

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

// Models lists provider/model-id values.
// A non-nil error may still come with ids from providers that could be listed.
func (a *App) Models(ctx context.Context) ([]string, error) {
	return a.cat.List(ctx)
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
	ref, err := provider.Parse(raw)
	if err != nil {
		return err
	}
	ep, err := a.cat.Endpoint(ref)
	if err != nil {
		return err
	}
	client := openai.NewEndpoint(ep.BaseURL, ep.APIKey, ep.Compat)
	if ep.Probe {
		if err := client.Show(ctx, ep.Model); err != nil {
			return fmt.Errorf("checking model %q: %w", ref.String(), err)
		}
	}
	if persist {
		if err := a.cat.SaveSelection(ref.String()); err != nil {
			return err
		}
	}

	if a.session == nil {
		recorder, history := a.openHistory(ref.String())
		a.history = history
		a.session = agent.NewSession(client, ep.Model, a.cwd, recorder)
		a.model = ref.String()
		return nil
	}

	a.session.SetModel(client, ep.Model)
	if a.model != ref.String() {
		a.noteModel(ref.String())
	}
	a.model = ref.String()
	return nil
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
