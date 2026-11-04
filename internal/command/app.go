package command

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kevinjom/co/internal/agent"
	"github.com/kevinjom/co/internal/config"
	"github.com/kevinjom/co/internal/lsp"
	"github.com/kevinjom/co/internal/openai"
	"github.com/kevinjom/co/internal/provider"
	"github.com/kevinjom/co/internal/transcript"
)

// running is one open conversation and its transcript.
type running struct {
	session *agent.Session
	history *transcript.Writer
}

// App is the catalog and the sessions shared by both fronts.
// The line reader uses the first session. The TUI can open more.
type App struct {
	cat           *provider.Catalog
	home          string
	cwd           string
	noHistory     bool
	maxToolRounds int

	running []running
	model   string
	lang    *lsp.Hub
}

// New starts an app that has no session until a model is activated.
// A maxToolRounds below 1 uses config.DefaultMaxToolRounds.
func New(cat *provider.Catalog, home, cwd string, noHistory bool, maxToolRounds int) *App {
	if maxToolRounds < 1 {
		maxToolRounds = config.DefaultMaxToolRounds
	}
	lang := lsp.New(cwd)
	if !lang.Available() {
		slog.Info("language tools disabled", "reason", "no language server on PATH")
	}
	return &App{
		cat:           cat,
		home:          home,
		cwd:           cwd,
		noHistory:     noHistory,
		maxToolRounds: maxToolRounds,
		running:       []running{},
		lang:          lang,
	}
}

// Ready reports whether a model has been activated.
func (a *App) Ready() bool {
	return len(a.running) > 0
}

// Model is the selected provider/model-id, or empty.
func (a *App) Model() string {
	return a.model
}

// Session is the first conversation. It is nil until the first successful selection.
func (a *App) Session() *agent.Session {
	if len(a.running) == 0 {
		return nil
	}
	return a.running[0].session
}

// Open starts another conversation on the current model.
// The first session still comes from Activate or Resume.
func (a *App) Open(ctx context.Context) (*agent.Session, error) {
	if strings.TrimSpace(a.model) == "" || len(a.running) == 0 {
		return nil, fmt.Errorf("select a model with /models")
	}
	client, wire, ref, err := a.openClient(ctx, a.model)
	if err != nil {
		return nil, err
	}
	return a.attach(client, wire, ref)
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
	return a.use(ctx, raw, false, nil)
}

// Select selects a model from /models and records it as the next launch default.
// A nil session retargets the first one, or creates it when none exists.
// Any other session is retargeted alone.
func (a *App) Select(ctx context.Context, raw string, session *agent.Session) error {
	return a.use(ctx, raw, true, session)
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
	if len(a.running) > 0 {
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
	var history *transcript.Writer
	if a.noHistory {
		if err := loaded.Writer.Close(); err != nil {
			slog.Warn("closing session history", "err", err)
		}
	} else {
		history = loaded.Writer
		recorder = loaded.Writer
	}
	session := a.newSession(client, wire, recorder)
	session.Restore(loaded.Messages)
	session.SetTitle(loaded.Title)
	a.running = append(a.running, running{session: session, history: history})
	a.model = ref
	if ref != loaded.Model {
		a.noteModel(session, ref)
	}
	return nil
}

// ExitNote tells the user how to resume each transcript this process wrote.
func (a *App) ExitNote() string {
	lines := []string{}
	for _, run := range a.running {
		if run.history == nil || !run.history.Persisted() {
			continue
		}
		lines = append(lines, "resume with: co --resume "+run.history.ID())
	}
	return strings.Join(lines, "\n")
}

// Close waits for chat titles, then closes every session transcript.
func (a *App) Close() {
	for _, run := range a.running {
		if run.session != nil {
			run.session.Wait()
		}
	}
	for i := range a.running {
		history := a.running[i].history
		if history == nil {
			continue
		}
		if err := history.Close(); err != nil {
			slog.Warn("closing session history", "err", err)
		}
		a.running[i].history = nil
	}
	if a.lang != nil {
		if err := a.lang.Close(); err != nil {
			slog.Warn("closing language server", "err", err)
		}
	}
}

func (a *App) use(ctx context.Context, raw string, persist bool, target *agent.Session) error {
	client, wire, ref, err := a.openClient(ctx, raw)
	if err != nil {
		return err
	}
	if persist {
		if err := a.cat.SaveSelection(ref); err != nil {
			return err
		}
	}

	if len(a.running) == 0 {
		_, err := a.attach(client, wire, ref)
		return err
	}
	if target == nil {
		target = a.running[0].session
	}
	if a.indexOf(target) < 0 {
		return fmt.Errorf("unknown session")
	}

	target.SetModel(client, wire)
	if a.model != ref {
		a.noteModel(target, ref)
	}
	a.model = ref
	return nil
}

func (a *App) attach(client *openai.Client, wire, ref string) (*agent.Session, error) {
	recorder, history := a.openHistory(ref)
	session := a.newSession(client, wire, recorder)
	a.running = append(a.running, running{session: session, history: history})
	a.model = ref
	return session, nil
}

func (a *App) newSession(client agent.Chatter, wire string, recorder agent.Recorder) *agent.Session {
	session := agent.NewSession(client, wire, a.cwd, recorder)
	session.SetMaxToolRounds(a.maxToolRounds)
	if a.lang != nil && a.lang.Available() {
		session.EnableLSP(a.lang)
	}
	return session
}

func (a *App) indexOf(session *agent.Session) int {
	for i := range a.running {
		if a.running[i].session == session {
			return i
		}
	}
	return -1
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

func (a *App) noteModel(session *agent.Session, model string) {
	index := a.indexOf(session)
	if index < 0 || a.running[index].history == nil {
		return
	}
	if err := a.running[index].history.RecordModel(model); err != nil {
		slog.Warn("session history disabled", "err", err)
		_ = a.running[index].history.Close()
		a.running[index].history = nil
		session.DropRecorder()
	}
}
