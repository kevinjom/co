package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"co/internal/agent"
	"co/internal/ui"

	"github.com/spf13/cobra"
)

func repl(cmd *cobra.Command, signals *signalSession, session *agent.Session, modelName, cwd string) error {
	if isTerminal(cmd.InOrStdin()) && isTerminal(cmd.OutOrStdout()) {
		err := ui.Run(signals.ctx, session, modelName, cwd)
		if err != nil && signals.ctx.Err() != nil {
			return signals.err()
		}
		return err
	}
	return lineRepl(cmd, signals, session)
}

func lineRepl(cmd *cobra.Command, signals *signalSession, session *agent.Session) error {
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
		if line == "exit" || line == "quit" {
			return nil
		}

		wrote := false
		answer, err := session.Turn(signals.ctx, line, func(ev agent.Event) {
			switch ev := ev.(type) {
			case agent.AnswerDelta:
				fmt.Fprint(cmd.OutOrStdout(), ev.Text)
				wrote = true
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
