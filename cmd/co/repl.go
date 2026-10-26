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

	"github.com/spf13/cobra"
)

func repl(cmd *cobra.Command, signals *signalSession, session *agent.Session) error {
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

		answer, err := session.Turn(signals.ctx, line, func(trace string) {
			fmt.Fprintln(cmd.ErrOrStderr(), trace)
		})
		if err != nil {
			if signals.ctx.Err() != nil {
				return signals.err()
			}
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), answer)
	}
}

func isTerminal(r io.Reader) bool {
	file, ok := r.(*os.File)
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
