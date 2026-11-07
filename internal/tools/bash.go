package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultBashTimeout = 2 * time.Minute
	maxBashOutput      = 32 * 1024
	bashBinary         = "/bin/bash"
)

type bashSpec struct {
	command string
	dir     string
	env     []string
	writes  []string
}

func bashSchema() Schema {
	return Schema{
		Name:        "bash",
		Description: bashDescription(),
		Parameters: objectSchema(
			map[string]any{
				"command": stringProp("Shell command to run."),
			},
			[]string{"command"},
		),
	}
}

func bashDescription() string {
	write := "It can write anywhere."
	if runtime.GOOS == "linux" {
		write = "It can write only inside the working directory and ~/.co."
	}
	return "Run a shell command. stdout and stderr come back with the exit code. " +
		"The command can read the rest of the disk and use the network. " +
		write +
		" A command still running after 2 minutes is killed."
}

func (s *Set) bash(ctx context.Context, args map[string]any) (string, error) {
	command, ok, err := requiredString(args, "command")
	if err != nil {
		return "", err
	}
	if !ok || strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command is required")
	}

	spec, err := s.bashSpec(command)
	if err != nil {
		return "", err
	}
	name, argv, err := bashCommand(spec)
	if err != nil {
		return "", err
	}

	timeout := s.bashTimeout
	if timeout <= 0 {
		timeout = defaultBashTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, argv...)
	cmd.Dir = spec.dir
	cmd.Env = spec.env
	return runCommand(ctx, cmd)
}

func (s *Set) bashSpec(command string) (bashSpec, error) {
	root, err := jailRoot(s.root)
	if err != nil {
		return bashSpec{}, err
	}
	coDir, scratch, err := bashScratch()
	if err != nil {
		return bashSpec{}, err
	}
	realCo, err := filepath.EvalSymlinks(coDir)
	if err != nil {
		return bashSpec{}, fmt.Errorf("config directory: %w", err)
	}
	return bashSpec{
		command: command,
		dir:     root,
		env:     bashEnv(scratch),
		writes:  []string{root, realCo},
	}, nil
}

func bashScratch() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("home directory: %w", err)
	}
	coDir := filepath.Join(home, ".co")
	scratch := filepath.Join(coDir, "bash")
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		return "", "", fmt.Errorf("creating scratch directory: %w", err)
	}
	if err := os.Chmod(coDir, 0o700); err != nil {
		return "", "", fmt.Errorf("creating scratch directory: %w", err)
	}
	if err := os.Chmod(scratch, 0o700); err != nil {
		return "", "", fmt.Errorf("creating scratch directory: %w", err)
	}
	return coDir, scratch, nil
}

func bashEnv(scratch string) []string {
	env := os.Environ()
	if env == nil {
		env = []string{}
	}
	env = replaceEnv(env, "TMPDIR", scratch)
	env = replaceEnv(env, "GOCACHE", scratch)
	return replaceEnv(env, "GOTMPDIR", scratch)
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	next := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		next = append(next, item)
	}
	return append(next, prefix+value)
}

func runCommand(ctx context.Context, cmd *exec.Cmd) (string, error) {
	configureCmd(cmd)
	pr, pw, err := os.Pipe()
	if err != nil {
		return "", err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("starting command: %w", err)
	}
	pw.Close()
	output, truncated, readErr := readTail(pr, maxBashOutput)
	pr.Close()
	waitErr := cmd.Wait()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if readErr != nil {
		return "", fmt.Errorf("reading command output: %w", readErr)
	}
	if waitErr == nil {
		return formatBash(0, output, truncated), nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return formatBash(exitErr.ExitCode(), output, truncated), nil
	}
	return "", fmt.Errorf("command: %w", waitErr)
}

func readTail(r io.Reader, max int) (string, bool, error) {
	buf := make([]byte, 0, max)
	truncated := false
	tmp := make([]byte, 32*1024)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if len(buf) > max {
				truncated = true
				tail := make([]byte, max)
				copy(tail, buf[len(buf)-max:])
				buf = tail
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", false, err
		}
	}
	if truncated {
		for len(buf) > 0 && !utf8.RuneStart(buf[0]) {
			buf = buf[1:]
		}
	}
	return string(buf), truncated, nil
}

func formatBash(code int, output string, truncated bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "exit: %d\n", code)
	if truncated {
		b.WriteString("truncated: true\n")
	}
	output = strings.TrimRight(output, "\n")
	if output != "" {
		b.WriteString(output)
	}
	return strings.TrimRight(b.String(), "\n")
}
