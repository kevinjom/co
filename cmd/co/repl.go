package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kevinjom/co/internal/agent"
	"github.com/kevinjom/co/internal/command"
	"github.com/kevinjom/co/internal/theme"
	"github.com/kevinjom/co/internal/tui"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

func repl(cmd *cobra.Command, signals *signalSession, app *command.App, cwd string, clearScrollback bool, themeName string) error {
	if isTerminal(cmd.InOrStdin()) && isTerminal(cmd.OutOrStdout()) {
		err := tui.Run(signals.ctx, app, cwd, clearScrollback, themeName)
		if err != nil && signals.ctx.Err() != nil {
			return signals.err()
		}
		return err
	}
	return lineRepl(cmd, signals, app)
}

func lineRepl(cmd *cobra.Command, signals *signalSession, app *command.App) error {
	in := cmd.InOrStdin()
	lines := newLineReader(in)
	interactive := isTerminal(in)

	for {
		if err := signals.ctx.Err(); err != nil {
			return signals.err()
		}
		if interactive {
			fmt.Fprint(cmd.ErrOrStderr(), "> ")
		}

		line, err := lines.read(signals.ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			if signals.ctx.Err() != nil {
				return signals.err()
			}
			return fmt.Errorf("reading input: %w", err)
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if handled, err := runLineCommand(cmd, signals, app, line); handled {
			if errors.Is(err, errQuit) {
				return nil
			}
			if err != nil {
				return err
			}
			continue
		}

		session := app.Session()
		if session == nil {
			return usageError(fmt.Errorf("model is required: set --model as provider/model-id, or set model in ~/.co/config.yaml"))
		}

		wrote := false
		answer, err := session.Turn(signals.ctx, line, func(ev agent.Event) {
			switch ev := ev.(type) {
			case agent.AnswerDelta:
				fmt.Fprint(cmd.OutOrStdout(), ev.Text)
				wrote = true
			case agent.ReasoningDelta:
				fmt.Fprint(cmd.ErrOrStderr(), ev.Text)
			case agent.ToolStart:
				fmt.Fprintf(cmd.ErrOrStderr(), "tool %s %s\n", ev.Name, ev.Detail)
			case agent.ToolDone:
				if ev.Err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "tool %s error: %s\n", ev.ID, ev.Err)
				}
			default:
				panic(fmt.Sprintf("unhandled agent event %T", ev))
			}
		})
		if wrote {
			fmt.Fprintln(cmd.OutOrStdout())
		} else if err == nil && answer != "" {
			fmt.Fprintln(cmd.OutOrStdout(), answer)
		}
		if err != nil {
			if signals.ctx.Err() != nil {
				return signals.err()
			}
			return err
		}
	}
}

func runLineCommand(cmd *cobra.Command, signals *signalSession, app *command.App, line string) (bool, error) {
	in := cmd.InOrStdin()
	interactive := isTerminal(in)
	parsed := command.Parse(line, skillNames(app))
	stderr := cmd.ErrOrStderr()
	switch parsed.Kind {
	case command.KindChat:
		return false, nil
	case command.KindQuit:
		if note := app.ExitNote(); note != "" {
			fmt.Fprintln(stderr, note)
		}
		return true, errQuit
	case command.KindError:
		fmt.Fprintln(stderr, parsed.Err)
		return true, nil
	case command.KindNew, command.KindSessions:
		fmt.Fprintln(stderr, "available in the TUI")
		return true, nil
	case command.KindTheme:
		if parsed.Arg == "" {
			for _, name := range theme.Names() {
				fmt.Fprintln(stderr, name)
			}
			return true, nil
		}
		name, err := theme.Parse(parsed.Arg)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return true, nil
		}
		if err := app.SaveTheme(name); err != nil {
			fmt.Fprintln(stderr, err)
			return true, nil
		}
		fmt.Fprintf(stderr, "theme %s\n", name)
		return true, nil
	case command.KindSkill:
		if parsed.Arg != "" {
			return false, nil
		}
		skills := app.Skills()
		if len(skills) == 0 {
			fmt.Fprintln(stderr, "no skills")
		}
		for _, sk := range skills {
			fmt.Fprintf(stderr, "%s: %s\n", sk.Name, oneLine(sk.Description))
		}
		return true, nil
	case command.KindModels:
		if parsed.Arg == "" {
			ids, err := app.Models(signals.ctx)
			for _, id := range ids {
				fmt.Fprintln(stderr, id)
			}
			if err != nil {
				fmt.Fprintln(stderr, err)
			}
			return true, nil
		}
		if err := app.Select(signals.ctx, parsed.Arg, app.Session()); err != nil {
			if signals.ctx.Err() != nil {
				return true, signals.err()
			}
			fmt.Fprintln(stderr, err)
			return true, nil
		}
		fmt.Fprintf(stderr, "model %s\n", app.Model())
		return true, nil
	case command.KindStats:
		session := app.Session()
		if session == nil {
			fmt.Fprintln(stderr, "select a model with /models")
			return true, nil
		}
		fmt.Fprintln(stderr, session.Stats().Text())
		return true, nil
	case command.KindConnect:
		if parsed.Arg == "" {
			ids := app.Providers()
			for _, id := range ids {
				fmt.Fprintln(stderr, id)
			}
			if len(ids) == 0 {
				fmt.Fprintln(stderr, "no providers accept an API key")
			}
			return true, nil
		}
		if !interactive {
			return true, usageError(app.RejectConnect(parsed.Arg))
		}
		if err := app.CanConnect(parsed.Arg); err != nil {
			fmt.Fprintln(stderr, err)
			return true, nil
		}
		fmt.Fprintf(stderr, "API key for %s: ", parsed.Arg)
		key, err := readSecret(in)
		fmt.Fprintln(stderr)
		if err != nil {
			return true, err
		}
		if err := app.Connect(parsed.Arg, key); err != nil {
			fmt.Fprintln(stderr, err)
			return true, nil
		}
		fmt.Fprintf(stderr, "saved key for %s\n", parsed.Arg)
		return true, nil
	default:
		panic(fmt.Sprintf("unhandled command kind %d", parsed.Kind))
	}
}

var errQuit = errors.New("quit")

func skillNames(app *command.App) []string {
	if app == nil {
		return []string{}
	}
	list := app.Skills()
	names := make([]string, 0, len(list))
	for _, sk := range list {
		names = append(names, sk.Name)
	}
	return names
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func readSecret(r io.Reader) (string, error) {
	file, ok := r.(*os.File)
	if !ok || !isTerminal(file) {
		return "", usageError(fmt.Errorf("connect: run /connect in a terminal"))
	}
	buf, err := term.ReadPassword(file.Fd())
	if err != nil {
		return "", fmt.Errorf("reading api key: %w", err)
	}
	return string(buf), nil
}

func isTerminal(v any) bool {
	file, ok := v.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

type readResult struct {
	line string
	err  error
}

type lineReader struct {
	req chan struct{}
	res chan readResult
}

func newLineReader(r io.Reader) *lineReader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	reader := &lineReader{
		req: make(chan struct{}),
		res: make(chan readResult),
	}
	go reader.loop(scanner)
	return reader
}

func (r *lineReader) loop(scanner *bufio.Scanner) {
	for range r.req {
		if !scanner.Scan() {
			err := scanner.Err()
			if err == nil {
				err = io.EOF
			}
			r.res <- readResult{err: err}
			return
		}
		r.res <- readResult{line: scanner.Text()}
	}
}

func (r *lineReader) read(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r.req <- struct{}{}:
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-r.res:
		return result.line, result.err
	}
}
